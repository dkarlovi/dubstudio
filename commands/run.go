package commands

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/haguro/elevenlabs-go"
	"github.com/hajimehoshi/go-mp3"
	"github.com/symfony-cli/console"
	"gopkg.in/yaml.v3"
)

type SpeakerConfig struct {
	Model    string  `yaml:"model"` // ElevenLabs voice ID
	Name     string  `yaml:"name"`
	Speed    float32 `yaml:"speed"`
	TTSModel string  `yaml:"tts_model"` // optional: ElevenLabs model ID override for this speaker
}

type Config struct {
	AuthKey               string                   `yaml:"auth_key"`
	TTSModel              string                   `yaml:"tts_model"` // optional: default ElevenLabs model ID for all speakers
	Default               SpeakerConfig            `yaml:"default"`
	Models                map[string]SpeakerConfig `yaml:"models"`
	MergeLinesThresholdMs int                      `yaml:"merge_lines_threshold_ms"` // optional
}

// defaultTTSModel is used when no tts_model is set anywhere in the config.
// Kept as the historical default so existing configs behave identically.
const defaultTTSModel = "eleven_multilingual_v2"

// resolveTTSModel picks the ElevenLabs model ID for a speaker, in order of
// precedence: per-speaker tts_model, top-level tts_model, built-in default.
func resolveTTSModel(speaker SpeakerConfig, cfg *Config) string {
	if speaker.TTSModel != "" {
		return speaker.TTSModel
	}
	if cfg.TTSModel != "" {
		return cfg.TTSModel
	}
	return defaultTTSModel
}

type Model struct {
	model    string
	name     string
	offset   int
	speed    float32
	ttsModel string
	// speedOverride is set when the speed came from a per-line @speed tag
	// rather than the speaker config. Such a line is a branch off the request
	// stitching chain, not a link in it -- see previousIdsFor.
	speedOverride bool
}

type Path struct {
	Path     string
	Template string
	Id       string
}

type Item struct {
	Sub        *astisub.Item
	Model      Model
	Path       Path
	MergedFrom []string // timings of merged-from lines
}

type AudioFile struct {
	Item     Item
	Duration time.Duration
	Offset   time.Duration
	Channel  int
	Overlap  time.Duration
}

// All returns all available commands
func All() []*console.Command {
	return []*console.Command{
		{
			Name:        "run",
			Usage:       "Convert subtitle files to audio using ElevenLabs TTS",
			Description: "Convert subtitle files to audio",
			Args: []*console.Arg{
				{
					Name:        "file",
					Description: "Path to the .srt or .vtt subtitle file",
				},
			},
			Flags: []console.Flag{
				&console.IntFlag{
					Name:    "merge-lines-threshold-ms",
					Aliases: []string{"m"},
					Usage:   "Merge lines if same speaker and gap is below this threshold (ms)",
				},
			},
			Action: Run,
		},
	}
}

func Run(c *console.Context) error {
	path := c.Args().Get("file")

	config, err := readConfig(c.String("config"))
	if err != nil {
		return console.Exit(fmt.Sprintf("Error reading config: %v", err), 1)
	}

	threshold := config.MergeLinesThresholdMs
	if c.Int("merge-lines-threshold-ms") > 0 {
		threshold = c.Int("merge-lines-threshold-ms")
	}
	if threshold > 0 {
		log.Printf("Using merge threshold: %d", threshold)
	} else {
		log.Printf("No merge threshold set, not merging lines")
	}

	items := parseSubtitleFile(config, path, threshold)

	client := elevenlabs.NewClient(context.Background(), config.AuthKey, 30*time.Second)
	audioFiles := generateMissingVoiceLines(client, items)

	overlapsByFirst := make(map[int]cueOverlap)
	for _, ov := range findOverlaps(audioFiles, 0) {
		overlapsByFirst[ov.First] = ov
	}

	overlaps := make([]AudioFile, 0)
	for i, file := range audioFiles {
		fileEndAt := file.Offset + file.Duration
		var overlapText string
		if ov, ok := overlapsByFirst[i]; ok {
			file.Overlap = ov.Duration
			overlapText = fmt.Sprintf(" (<fg=yellow>OVERLAP %s</>)", file.Overlap.Round(time.Millisecond))
			overlaps = append(overlaps, file)
		}

		fmt.Fprintf(c.App.Writer,
			"#%03d\n<info>%s</>\nSpeaker:  <comment>%s</>, speed: %.2f\nSubtitle: <fg=yellow>%s</> --> <fg=yellow>%s</> (duration <fg=yellow>%s</>)\nAudio:    <fg=yellow>%s</> --> <fg=yellow>%s</> (duration <fg=yellow>%s</>)%s\nPath:     <fg=default>%s</>\n",
			file.Item.Sub.Index+1,
			file.Item.Sub.String(),
			file.Item.Model.name,
			file.Item.Model.speed,
			file.Item.Sub.StartAt.Round(time.Millisecond),
			file.Item.Sub.EndAt.Round(time.Millisecond),
			(file.Item.Sub.EndAt - file.Item.Sub.StartAt).Round(time.Millisecond),
			file.Offset.Round(time.Millisecond),
			fileEndAt.Round(time.Millisecond),
			file.Duration.Round(time.Millisecond),
			overlapText,
			file.Item.Path.Path,
		)
		// Print merged-from info if present
		if len(file.Item.MergedFrom) > 1 {
			fmt.Fprintf(c.App.Writer, "Merged from:\n")
			for _, line := range file.Item.MergedFrom {
				parts := strings.SplitN(line, " | ", 2)
				if len(parts) == 2 {
					fmt.Fprintf(c.App.Writer,
						"    %s\n    %s\n",
						parts[1], parts[0],
					)
				} else {
					fmt.Fprintf(c.App.Writer, "    %s\n", line)
				}
			}
		}
		fmt.Fprintf(c.App.Writer, "\n")
	}

	if len(overlaps) > 0 {
		fmt.Fprintf(c.App.Writer, "<fg=yellow>Overlaps detected:</>\n")
		for _, overlap := range overlaps {
			fmt.Fprintf(c.App.Writer,
				"#%03d <fg=yellow>%s</>\n<info>%s</>\n\n",
				overlap.Item.Sub.Index+1,
				overlap.Overlap.Round(time.Millisecond),
				overlap.Item.Sub.String(),
			)
		}
		fmt.Fprintf(c.App.Writer, "Fix and rerun the script to generate the final audio file.\n")
		os.Exit(1)
	}

	outputPath := strings.TrimSuffix(path, filepath.Ext(path)) + "_" + time.Now().Format("2006-01-02-15-04-05") + ".wav"
	if err := generateFinalAudioFile(audioFiles, outputPath); err != nil {
		return console.Exit(fmt.Sprintf("Error writing final audio track: %v", err), 1)
	}
	log.Printf("Final audio track written to %s\n", outputPath)
	return nil
}

func readConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		var typeError *yaml.TypeError
		if errors.As(err, &typeError) {
			msg := ""
			for _, field := range typeError.Errors {
				msg += fmt.Sprintf("  - <fg=red>%s</>\n", field)
			}
			return nil, fmt.Errorf("error parsing config file <info>%s</>:\n%s", filename, msg)
		}
		return nil, err
	}
	return &config, nil
}

func generateModelChannelMap(config *Config) map[string]int {
	channels := make(map[string]int)
	// Default model always goes to channel 0
	channels[config.Default.Name] = 0

	currentChannel := 1
	// Assign unique channels to each distinct model
	for _, model := range config.Models {
		if _, exists := channels[model.Name]; !exists {
			channels[model.Name] = currentChannel
			currentChannel++
		}
	}
	return channels
}

func generatePathTemplate(root string, item *astisub.Item, model Model) Path {
	re := regexp.MustCompile(`[,.!?'<>:"/\\|?*\x00-\x1F]`)
	dialog := re.ReplaceAllString(item.String(), "")
	dialog = strings.ToLower(dialog)
	dialog = strings.Replace(dialog, " ", "_", -1)
	dialog = strings.TrimSpace(dialog)
	if len(dialog) > 50 {
		dialog = dialog[:50]
	}

	// Everything that changes the produced audio goes into the checksum: voice
	// ID, TTS model, effective speed (per-speaker or per-line) and the text. A
	// cache hit is therefore just this hash plus a lookup on disk.
	checksum := md5.Sum([]byte(model.model + model.ttsModel + fmt.Sprintf("%f", model.speed) + item.String()))
	template := filepath.Join(root, fmt.Sprintf("%X-%s-%s.%%s.mp3", checksum[:4], model.name, dialog))

	glob := fmt.Sprintf(template, "*")
	if files, err := filepath.Glob(glob); err == nil && len(files) > 0 {
		// found the previously generated file, extract the ID out of it
		re := regexp.MustCompile(`([^.]+).mp3$`)
		match := re.FindStringSubmatch(filepath.Base(files[0]))
		if len(match) > 1 {
			return Path{Path: files[0], Template: template, Id: match[1]}
		}
	}

	return Path{Template: template}
}

func parseSubtitleFile(config *Config, path string, mergeLinesThresholdMs int) []Item {
	subs, err := astisub.OpenFile(path)
	if err != nil {
		log.Fatalf("Error parsing VTT file: %v", err)
	}

	modelChannels := generateModelChannelMap(config)
	items := make([]Item, 0)
	root, _ := filepath.Abs(filepath.Dir(path))

	// Merge logic
	type mergedResult struct {
		item       *astisub.Item
		mergedFrom []string
	}
	mergedSubs := make([]mergedResult, 0)
	i := 0
	for i < len(subs.Items) {
		cur := subs.Items[i]
		// Determine speaker for current line
		var curSpeaker string
		if cur.Lines[0].VoiceName != "" {
			curSpeaker = cur.Lines[0].VoiceName
		} else if len(cur.Comments) > 0 {
			curSpeaker = cur.Comments[0]
		} else {
			curSpeaker, _, _ = resolveSpeaker(cur.String())
		}
		// Prepare to merge into a single line
		mergedText := cur.String()
		mergedStart := cur.StartAt
		mergedEnd := cur.EndAt
		mergedVoiceName := cur.Lines[0].VoiceName
		mergedComments := cur.Comments
		mergedFrom := []string{
			fmt.Sprintf("<fg=yellow>%s</> --> <fg=yellow>%s</> (duration <fg=yellow>%s</>) | <info>%s</>",
				cur.StartAt.Round(time.Millisecond),
				cur.EndAt.Round(time.Millisecond),
				(cur.EndAt - cur.StartAt).Round(time.Millisecond),
				strings.TrimSpace(cur.String()),
			),
		}
		for {
			// Try to merge with next lines if threshold is set
			if mergeLinesThresholdMs > 0 && i+1 < len(subs.Items) {
				next := subs.Items[i+1]
				var nextSpeaker string
				if next.Lines[0].VoiceName != "" {
					nextSpeaker = next.Lines[0].VoiceName
				} else if len(next.Comments) > 0 {
					nextSpeaker = next.Comments[0]
				} else {
					nextSpeaker, _, _ = resolveSpeaker(next.String())
				}
				gap := next.StartAt - mergedEnd
				if curSpeaker == nextSpeaker && gap.Milliseconds() >= 0 && gap.Milliseconds() <= int64(mergeLinesThresholdMs) {
					// Merge: extend end time, concat text
					mergedEnd = next.EndAt
					mergedText = strings.TrimSpace(mergedText) + " " + strings.TrimSpace(next.String())
					mergedFrom = append(mergedFrom, fmt.Sprintf("<fg=yellow>%s</> --> <fg=yellow>%s</> (duration <fg=yellow>%s</>) | <info>%s</>",
						next.StartAt.Round(time.Millisecond),
						next.EndAt.Round(time.Millisecond),
						(next.EndAt-next.StartAt).Round(time.Millisecond),
						strings.TrimSpace(next.String()),
					))
					i++
					continue
				}
			}
			break
		}
		// Create a new astisub.Item with the merged text as a single line
		mergedItem := &astisub.Item{
			StartAt: mergedStart,
			EndAt:   mergedEnd,
			Lines: []astisub.Line{
				{
					VoiceName: mergedVoiceName,
					Items: []astisub.LineItem{
						{Text: mergedText},
					},
				},
			},
			Comments: mergedComments,
		}
		mergedSubs = append(mergedSubs, mergedResult{item: mergedItem, mergedFrom: mergedFrom})
		i++
	}

	for i, res := range mergedSubs {
		sub := res.item
		sub.Index = i
		var modelName string
		if sub.Lines[0].VoiceName != "" {
			modelName = sub.Lines[0].VoiceName
		} else if len(sub.Comments) > 0 {
			modelName = sub.Comments[0]
		} else if name, dialogue, tagged := resolveSpeaker(sub.String()); tagged {
			modelName = name
			sub.Lines[0].Items[0].Text = dialogue
		}

		// A speaker tag may carry a per-line speed, e.g. [Matko@1.15] or, for
		// the default speaker, [@1.15]. Split it off before looking the speaker
		// up, so "Matko@1.15" resolves against the configured "Matko".
		modelName, lineSpeed, hasLineSpeed, specErr := splitSpeakerSpec(modelName)
		if specErr != nil {
			log.Fatalf("Error in subtitle #%d: %v", i+1, specErr)
		}

		var model Model
		if modelName != "" {
			modelConfig, err := lookupSpeakerModel(modelName, config)
			if err != nil {
				log.Fatalf("Error in subtitle #%d: %v", i+1, err)
			}
			model = Model{name: modelConfig.Name, model: modelConfig.Model, offset: modelChannels[modelName], speed: modelConfig.Speed, ttsModel: resolveTTSModel(modelConfig, config)}
		} else {
			model = Model{name: config.Default.Name, model: config.Default.Model, offset: 0, speed: config.Default.Speed, ttsModel: resolveTTSModel(config.Default, config)}
		}

		// Applied before generatePathTemplate: the effective speed is already
		// part of the cache checksum, so each speed of a line is its own file
		// and every take stays on disk.
		if hasLineSpeed {
			model.speed = lineSpeed
			model.speedOverride = true
		}

		item := Item{
			Sub:        sub,
			Model:      model,
			Path:       generatePathTemplate(root, sub, model),
			MergedFrom: res.mergedFrom,
		}

		items = append(items, item)
	}

	return items
}

// previousIdsFor picks the request IDs this line should stitch onto.
//
// A line carrying a per-cue @speed override is an escape hatch rather than a
// link in the chain: lines at the speaker's normal speed step over it, so
// changing one line's speed never invalidates the takes that follow. Lines
// sharing the same override chain onto each other. When nothing matching is on
// disk we fall back to the normal-speed chain, which is what the previous
// unconditional lookback did.
func previousIdsFor(items []Item, item Item, want, lookback int) []string {
	matching := make([]string, 0, want)
	chain := make([]string, 0, want)

	for i := item.Sub.Index - 1; i >= 0 && item.Sub.Index-i <= lookback; i-- {
		id := items[i].Path.Id
		if id == "" {
			continue
		}
		if !items[i].Model.speedOverride && len(chain) < want {
			chain = append(chain, id)
		}
		if items[i].Model.speedOverride == item.Model.speedOverride &&
			items[i].Model.speed == item.Model.speed {
			matching = append(matching, id)
			if len(matching) == want {
				return matching
			}
		}
	}

	if len(matching) > 0 {
		return matching
	}
	return chain
}

func generateMissingVoiceLines(client *elevenlabs.Client, items []Item) []AudioFile {
	audioFiles := make([]AudioFile, 0)
	for _, item := range items {
		if item.Path.Path != "" {
			duration, err := readAudioFileDuration(item.Path.Path)
			if err != nil {
				log.Fatalf("Error reading audio file %s duration: %v\n", item.Path.Path, err)
			}
			audioFiles = append(audioFiles, AudioFile{
				Item:     item,
				Offset:   item.Sub.StartAt,
				Channel:  item.Model.offset,
				Duration: duration,
			})
			continue
		}

		previousRequestIds := previousIdsFor(items, item, 3, 10)

		nextRequestIds := make([]string, 0)
		nextText := ""
		for i := item.Sub.Index + 1; i <= item.Sub.Index+3; i++ {
			if i >= len(items) {
				continue
			}
			if items[i].Path.Id == "" {
				nextText = items[i].Sub.String()
				break
			}

			nextRequestIds = append(nextRequestIds, items[i].Path.Id)
		}

		log.Printf("Speaking (as %s via %s) \"%s\"\n", item.Model.name, item.Model.ttsModel, item.Sub.String())
		ttsReq := elevenlabs.TextToSpeechRequest{
			VoiceSettings: &elevenlabs.VoiceSettings{
				SpeakerBoost: true,
				Speed:        item.Model.speed,
			},
			Text:               item.Sub.String(),
			ModelID:            item.Model.ttsModel,
			PreviousRequestIds: previousRequestIds,
			NextRequestIds:     nextRequestIds,
			NextText:           nextText,
		}

		speech, id, err := client.TextToSpeechWithRequestID(item.Model.model, ttsReq)
		if err != nil {
			log.Fatal(err)
		}

		path := fmt.Sprintf(item.Path.Template, id)
		if err := os.WriteFile(path, speech, 0644); err != nil {
			log.Fatal(err)
		}
		log.Printf("Wrote %s\n", path)
		item.Path.Path = path

		duration, err := readAudioFileDuration(path)
		if err != nil {
			log.Fatalf("Error reading audio file %s duration: %v\n", item.Path.Path, err)
		}

		audioFiles = append(audioFiles, AudioFile{
			Item:     item,
			Offset:   item.Sub.StartAt,
			Channel:  item.Model.offset,
			Duration: duration,
		})
	}

	return audioFiles
}

func readAudioFileDuration(path string) (time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	decoder, err := mp3.NewDecoder(f)
	if err != nil {
		return 0, err
	}

	duration := float64(decoder.Length()) / (4 * float64(decoder.SampleRate()))
	return time.Duration(duration * float64(time.Second)), nil
}

func generateFinalAudioFile(files []AudioFile, outputPath string) error {
	const sampleRate = 44100
	const bitDepth = 16

	numChannels := 0
	for _, file := range files {
		numChannels = max(numChannels, file.Channel+1)
	}

	var maxEndTime time.Duration
	for _, file := range files {
		maxEndTime = max(maxEndTime, file.Offset+file.Duration)
	}

	totalFrames := int(maxEndTime.Seconds() * float64(sampleRate))
	totalSamples := totalFrames * numChannels
	mixBuffer := &audio.IntBuffer{
		Format: &audio.Format{
			NumChannels: numChannels,
			SampleRate:  sampleRate,
		},
		Data:           make([]int, totalSamples),
		SourceBitDepth: bitDepth,
	}

	for _, file := range files {
		path := file.Item.Path.Path
		f, err := os.Open(path)
		defer f.Close()
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", path, err)
		}

		decoder, err := mp3.NewDecoder(f)
		if err != nil {
			return fmt.Errorf("failed to create decoder for %s: %w", path, err)
		}

		startFrame := int(file.Offset.Seconds() * float64(sampleRate))
		tmpBuf := make([]byte, 4096)
		currentFrame := 0
		for {
			n, err := decoder.Read(tmpBuf)
			if n > 0 {
				// Process samples in pairs of bytes (16-bit samples, 2 channels)
				for i := 0; i < n-1; i += 4 {
					frame := startFrame + (currentFrame / 4)
					if frame < totalFrames {
						sample := int(int16(tmpBuf[i]) | int16(tmpBuf[i+1])<<8)
						pos := (frame * numChannels) + file.Channel
						if pos < len(mixBuffer.Data) {
							mixBuffer.Data[pos] += sample
						}
					}
					currentFrame += 4
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("failed to read audio data: %w", err)
			}
		}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	enc := wav.NewEncoder(out, sampleRate, bitDepth, numChannels, 1)
	defer enc.Close()

	return enc.Write(mixBuffer)
}

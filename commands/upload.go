package commands

import (
	"fmt"

	"github.com/asticode/go-astisub"
)

// sessionCuesFromSubtitles parses a subtitle file's cues one-for-one (no
// merging -- that's the engine's own job at Generate time via -m), mirroring
// dub-studio's core.parse_vtt but general: voice/speed resolution reuses the
// same config-driven lookup the engine itself uses (resolveSpeaker,
// splitSpeakerSpec, lookupSpeakerModel), rather than Python's hardcoded
// matko/hana pair, and -- since the engine already supports it -- an
// originally-authored per-line [Name@speed] tag is honored at upload time
// instead of being silently ignored the way Python's simplified parser does.
func sessionCuesFromSubtitles(subs *astisub.Subtitles, config *Config) ([]*SessionCue, error) {
	cues := make([]*SessionCue, 0, len(subs.Items))
	for i, item := range subs.Items {
		var speakerSpec string
		if item.Lines[0].VoiceName != "" {
			speakerSpec = item.Lines[0].VoiceName
		} else if len(item.Comments) > 0 {
			speakerSpec = item.Comments[0]
		}

		dialogue := item.String()
		if speakerSpec == "" {
			if name, text, tagged := resolveSpeaker(item.String()); tagged {
				speakerSpec = name
				dialogue = text
			}
		}

		modelName, lineSpeed, hasLineSpeed, err := splitSpeakerSpec(speakerSpec)
		if err != nil {
			return nil, fmt.Errorf("cue #%d: %w", i+1, err)
		}

		var voiceName string
		var speed float32
		if modelName != "" {
			sc, err := lookupSpeakerModel(modelName, config)
			if err != nil {
				return nil, fmt.Errorf("cue #%d: %w", i+1, err)
			}
			voiceName = sc.Name
			speed = sc.Speed
		} else {
			voiceName = config.Default.Name
			speed = config.Default.Speed
		}
		if hasLineSpeed {
			speed = lineSpeed
		}

		cues = append(cues, &SessionCue{
			Index:       i + 1,
			StartMs:     int(item.StartAt.Milliseconds()),
			EndMs:       int(item.EndAt.Milliseconds()),
			Voice:       voiceName,
			SourceText:  dialogue,
			CurrentText: dialogue,
			// Rounded to 2dp: speed is always authored/configured to 2dp, but
			// float32 (the engine's own type, matching ElevenLabs' API) widens
			// imprecisely to float64 (e.g. 1.15 -> 1.149999976158142).
			Speed:         round2(float64(speed)),
			SpeedOverride: hasLineSpeed,
		})
	}
	return cues, nil
}

// NewSessionFromSubtitleFile parses a subtitle file into a fresh Session.
// If any cue has a tag that isn't at its very start, no session is built --
// callers must reject the upload and report the violations, mirroring
// dub-studio's app.py /upload handler.
func NewSessionFromSubtitleFile(name, path string, config *Config) (*Session, []TagViolation, error) {
	subs, err := astisub.OpenFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing subtitle file: %w", err)
	}

	if violations := findMidCueTagViolations(subs); len(violations) > 0 {
		return nil, violations, nil
	}

	cues, err := sessionCuesFromSubtitles(subs, config)
	if err != nil {
		return nil, nil, err
	}
	if len(cues) == 0 {
		return nil, nil, fmt.Errorf("no cues found in %s", path)
	}

	return &Session{Name: name, Cues: cues}, nil, nil
}

package commands

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/haguro/elevenlabs-go"
)

// ServiceConfig is dub-studio's own app-level policy layered on top of the
// engine's config.yaml (auth key, voice models) -- tolerance/margin/speed
// caps, merge/overlap knobs, and where a session's working files live.
type ServiceConfig struct {
	Engine             *Config
	WorkDir            string
	MergeThresholdMs   int
	MergeMaxMs         int
	OverlapToleranceMs int
	NormalizeTargetDb  int
	AutoFix            AutoFixConfig
	RunCap             int // 0 = unlimited
}

// ExportResult reports the outcome of an Export call, mirroring dub-studio's
// app.py /export manifest.
type ExportResult struct {
	WavPath         string `json:"wav_path"`
	StillOverBudget []int  `json:"still_over_budget"`
	Basket          []int  `json:"basket"`
}

// Service is the orchestration contract for the stepped upload/cleanup/
// generate/autofix/export workflow dub-studio's app.py drives today over
// HTTP. LocalService below is the "files first" implementation -- it runs
// entirely in-process against the local engine. A future HTTP-backed
// implementation (or a mock, for testing) can satisfy the same interface,
// so callers -- the file-driven CLI today, an HTTP layer later -- don't
// need to change when the implementation does.
type Service interface {
	Upload(name, path string) (*Session, []TagViolation, error)
	Cleanup(s *Session) []CueEdit
	Generate(s *Session) (GenerateResult, error)
	AutoFix(s *Session) AutoFixResult
	Export(s *Session) (ExportResult, error)
	UpdateCue(s *Session, index int, text *string, speed *float64) (*SessionCue, error)
}

// LocalService implements Service by calling the engine's own parsing,
// generation, overlap-detection and mixing functions in-process. This is
// what replaces dub-studio's current approach of shelling out to a
// separately-built srt11 binary and scraping its printed report.
type LocalService struct {
	cfg ServiceConfig
}

func NewLocalService(cfg ServiceConfig) *LocalService {
	return &LocalService{cfg: cfg}
}

func (ls *LocalService) Upload(name, path string) (*Session, []TagViolation, error) {
	return NewSessionFromSubtitleFile(name, path, ls.cfg.Engine)
}

func (ls *LocalService) Cleanup(s *Session) []CueEdit {
	texts := make([]string, len(s.Cues))
	for i, c := range s.Cues {
		texts[i] = c.CurrentText
	}
	edits := mechanicalCleanup(texts)
	for _, e := range edits {
		s.Cues[e.Index-1].CurrentText = e.After
	}
	// Marks cleanup as done for this session, mirroring dub-studio's
	// app.py setting Session.first_pass_report after /cleanup -- callers
	// (e.g. the legacy frontend) use its presence to advance past the
	// "uploaded" phase, distinct from whether any cue's text actually
	// changed.
	s.FirstPassReport = map[string]any{"cleanup": edits}
	return edits
}

func (ls *LocalService) sessionVTTPath() string {
	return filepath.Join(ls.cfg.WorkDir, "_session.vtt")
}

func (ls *LocalService) writeAndParse(cues []*SessionCue) ([]Item, error) {
	vttPath := ls.sessionVTTPath()
	if err := os.WriteFile(vttPath, writeSessionVTT(cues, ls.cfg.Engine.Default.Name), 0o644); err != nil {
		return nil, fmt.Errorf("writing session vtt: %w", err)
	}
	return parseSubtitleFile(ls.cfg.Engine, vttPath, ls.cfg.MergeThresholdMs, ls.cfg.MergeMaxMs), nil
}

func (ls *LocalService) elevenLabsClient() *elevenlabs.Client {
	return elevenlabs.NewClient(context.Background(), ls.cfg.Engine.AuthKey, 30*time.Second)
}

// Generate is THE function that spends a run: it enforces the run cap,
// then relies entirely on the engine's own content-hashed on-disk cache to
// make an unchanged cue free even though the whole cue list is always sent
// (full context is required for correct merging/stitching). Ported from
// dub-studio's core.generate_cues, adapted from subprocess+report-text
// scraping to direct in-process calls and struct access.
func (ls *LocalService) Generate(s *Session) (GenerateResult, error) {
	if ls.cfg.RunCap > 0 && s.RunCount >= ls.cfg.RunCap {
		return GenerateResult{}, fmt.Errorf(
			"run cap reached (%d generations for this video); no audio was generated, reset the session to continue", ls.cfg.RunCap)
	}

	items, err := ls.writeAndParse(s.Cues)
	if err != nil {
		return GenerateResult{}, err
	}

	skippedCached := 0
	for _, item := range items {
		if item.Path.Path != "" {
			skippedCached++
		}
	}

	audioFiles := generateMissingVoiceLines(ls.elevenLabsClient(), items)

	overlapTolerance := time.Duration(ls.cfg.OverlapToleranceMs) * time.Millisecond
	overlapsByFirst, overlaps := annotateOverlaps(audioFiles, findOverlaps(audioFiles, overlapTolerance))

	s.Cues = rebuildCuesAfterGenerate(s.Cues, audioFiles, overlapsByFirst)

	generatedCount := len(items) - skippedCached
	if generatedCount > 0 {
		s.RunCount++
	}

	generated := make([]int, 0, generatedCount)
	for i, item := range items {
		if item.Path.Path == "" {
			generated = append(generated, i+1)
		}
	}

	overlapping := make([]int, 0)
	for _, c := range s.Cues {
		if c.OverlapFlagged {
			overlapping = append(overlapping, c.Index)
		}
	}

	return GenerateResult{
		RunNumber:       s.RunCount,
		Generated:       generated,
		SkippedCached:   skippedCached,
		OverlapConflict: len(overlaps) > 0,
		OverlappingCues: overlapping,
	}, nil
}

func (ls *LocalService) AutoFix(s *Session) AutoFixResult {
	afCues := toAutoFixCues(s.Cues)
	result := AutoFixDurations(afCues, ls.cfg.AutoFix)
	applyAutoFixResults(s.Cues, afCues)
	return result
}

// buildExportResult decides whether a same-speaker overlap blocks export,
// and if not, summarizes what's still over budget or basketed. Pure and
// separable from the network call so it can be unit-tested with
// constructed AudioFile fixtures. Ported from dub-studio's core.export_audio.
func buildExportResult(cues []*SessionCue, overlaps []cueOverlap) (ExportResult, error) {
	if len(overlaps) > 0 {
		return ExportResult{}, fmt.Errorf(
			"export blocked: same-speaker cues still overlap past tolerance; fix or re-speed the flagged lines and Generate again first")
	}
	stillOver := make([]int, 0)
	basket := make([]int, 0)
	for _, c := range cues {
		if c.AudioMs != nil && c.OverageMs() > 0 {
			stillOver = append(stillOver, c.Index)
		}
		if c.NeedsHuman {
			basket = append(basket, c.Index)
		}
	}
	return ExportResult{StillOverBudget: stillOver, Basket: basket}, nil
}

// Export re-runs the identical generation as Generate (free: the cache
// means a matching prior generate already paid for every cue) so the
// exported audio matches exactly what was measured, then mixes the final
// WAV if nothing overlaps past tolerance. Ported from dub-studio's
// core.export_audio.
func (ls *LocalService) Export(s *Session) (ExportResult, error) {
	items, err := ls.writeAndParse(s.Cues)
	if err != nil {
		return ExportResult{}, err
	}
	audioFiles := generateMissingVoiceLines(ls.elevenLabsClient(), items)

	overlapTolerance := time.Duration(ls.cfg.OverlapToleranceMs) * time.Millisecond
	overlaps := findOverlaps(audioFiles, overlapTolerance)

	result, err := buildExportResult(s.Cues, overlaps)
	if err != nil {
		return ExportResult{}, err
	}

	outputPath := filepath.Join(ls.cfg.WorkDir,
		s.Name+"_"+time.Now().Format("2006-01-02-15-04-05")+".wav")
	if err := generateFinalAudioFile(audioFiles, outputPath, float64(ls.cfg.NormalizeTargetDb)); err != nil {
		return ExportResult{}, fmt.Errorf("writing final audio track: %w", err)
	}
	s.ExportWavPath = outputPath
	result.WavPath = outputPath
	return result, nil
}

// UpdateCue mirrors dub-studio's app.py POST /cue/{index}: a manual text
// edit clears the basket flag (a human already looked at it), and a speed
// edit is always treated as an override so it's tagged on the next
// Generate instead of silently tracking the speaker's configured default.
func (ls *LocalService) UpdateCue(s *Session, index int, text *string, speed *float64) (*SessionCue, error) {
	c := cueByIndex(s.Cues, index)
	if c == nil {
		return nil, fmt.Errorf("no cue #%d", index)
	}
	if text != nil {
		c.CurrentText = *text
		c.NeedsHuman = false
		c.HumanReason = ""
	}
	if speed != nil {
		c.Speed = math.Max(float64(MinSpeed), math.Min(float64(MaxSpeed), *speed))
		c.SpeedOverride = true
	}
	return c, nil
}

func toAutoFixCues(cues []*SessionCue) []*AutoFixCue {
	out := make([]*AutoFixCue, 0, len(cues))
	for _, c := range cues {
		out = append(out, &AutoFixCue{
			Index: c.Index, StartMs: c.StartMs, EndMs: c.EndMs, Voice: c.Voice,
			Speed: c.Speed, SpeedOverride: c.SpeedOverride, AudioMs: c.AudioMs,
			OverlapFlagged: c.OverlapFlagged,
		})
	}
	return out
}

func applyAutoFixResults(cues []*SessionCue, afCues []*AutoFixCue) {
	byIndex := make(map[int]*AutoFixCue, len(afCues))
	for _, af := range afCues {
		byIndex[af.Index] = af
	}
	for _, c := range cues {
		if af, ok := byIndex[c.Index]; ok {
			c.StartMs = af.StartMs
			c.Speed = af.Speed
			c.SpeedOverride = af.SpeedOverride
			c.NeedsHuman = af.NeedsHuman
			c.HumanReason = af.HumanReason
		}
	}
}

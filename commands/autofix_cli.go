package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/symfony-cli/console"
)

// autoFixCueJSON is the wire shape for the autofix-parity bridge: plain
// JSON in, plain JSON out, so the Python PoC can feed it the exact same
// fixtures as tests/test_scheduling.py without any VTT round trip.
type autoFixCueJSON struct {
	Index          int     `json:"index"`
	StartMs        int     `json:"start_ms"`
	EndMs          int     `json:"end_ms"`
	Voice          string  `json:"voice"`
	Speed          float64 `json:"speed"`
	AudioMs        *int    `json:"audio_ms"`
	OverlapFlagged bool    `json:"overlap_flagged"`
}

type autoFixConfigJSON struct {
	ToleranceMs     int                `json:"tolerance_ms"`
	AutofixMarginMs int                `json:"autofix_margin_ms"`
	SpeedCaps       map[string]float64 `json:"speed_caps"`
}

type autoFixCueResultJSON struct {
	Index         int     `json:"index"`
	StartMs       int     `json:"start_ms"`
	Speed         float64 `json:"speed"`
	SpeedOverride bool    `json:"speed_override"`
	NeedsHuman    bool    `json:"needs_human"`
	HumanReason   string  `json:"human_reason"`
}

func autoFixParitySummary(cuesPath, configPath string) ([]byte, error) {
	cuesRaw, err := os.ReadFile(cuesPath)
	if err != nil {
		return nil, fmt.Errorf("reading cues file: %w", err)
	}
	var inputCues []autoFixCueJSON
	if err := json.Unmarshal(cuesRaw, &inputCues); err != nil {
		return nil, fmt.Errorf("parsing cues JSON: %w", err)
	}

	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading autofix config file: %w", err)
	}
	var cfgJSON autoFixConfigJSON
	if err := json.Unmarshal(configRaw, &cfgJSON); err != nil {
		return nil, fmt.Errorf("parsing autofix config JSON: %w", err)
	}

	cues := make([]*AutoFixCue, 0, len(inputCues))
	for _, ic := range inputCues {
		cues = append(cues, &AutoFixCue{
			Index:          ic.Index,
			StartMs:        ic.StartMs,
			EndMs:          ic.EndMs,
			Voice:          ic.Voice,
			Speed:          ic.Speed,
			AudioMs:        ic.AudioMs,
			OverlapFlagged: ic.OverlapFlagged,
		})
	}

	result := AutoFixDurations(cues, AutoFixConfig{
		ToleranceMs:     cfgJSON.ToleranceMs,
		AutofixMarginMs: cfgJSON.AutofixMarginMs,
		SpeedCaps:       cfgJSON.SpeedCaps,
	})

	cuesOut := make([]autoFixCueResultJSON, 0, len(cues))
	for _, c := range cues {
		cuesOut = append(cuesOut, autoFixCueResultJSON{
			Index:         c.Index,
			StartMs:       c.StartMs,
			Speed:         c.Speed,
			SpeedOverride: c.SpeedOverride,
			NeedsHuman:    c.NeedsHuman,
			HumanReason:   c.HumanReason,
		})
	}

	payload := map[string]any{
		"cues":       cuesOut,
		"sped":       result.Sped,
		"basketed":   result.Basketed,
		"retimed":    result.Retimed,
		"still_open": result.StillOpen,
	}
	return json.MarshalIndent(payload, "", "  ")
}

func runAutoFixParity(c *console.Context) error {
	cuesPath := c.String("cues")
	if cuesPath == "" {
		return console.Exit("Missing --cues path", 1)
	}
	configPath := c.String("autofix-config")
	if configPath == "" {
		return console.Exit("Missing --autofix-config path", 1)
	}

	payload, err := autoFixParitySummary(cuesPath, configPath)
	if err != nil {
		return console.Exit(fmt.Sprintf("Error generating autofix parity summary: %v", err), 1)
	}

	outDir := c.String("out-dir")
	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return console.Exit(fmt.Sprintf("Error creating output dir: %v", err), 1)
	}

	outPath := filepath.Join(outDir, "autofix-parity-summary.json")
	if err := os.WriteFile(outPath, payload, 0o644); err != nil {
		return console.Exit(fmt.Sprintf("Error writing autofix parity summary: %v", err), 1)
	}

	fmt.Fprintf(c.App.Writer, "%s\n", payload)
	fmt.Fprintf(c.App.Writer, "Wrote %s\n", outPath)
	return nil
}

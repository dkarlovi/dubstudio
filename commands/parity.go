package commands

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/asticode/go-astisub"
	"github.com/symfony-cli/console"
)

func paritySummary(items []Item, violations []TagViolation) ([]byte, error) {
	cues := make([]map[string]any, 0, len(items))
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Sub == nil {
			continue
		}

		text := strings.TrimSpace(item.Sub.String())
		texts = append(texts, text)
		entry := map[string]any{
			"index":       item.Sub.Index + 1,
			"speaker":     item.Model.name,
			"text":        text,
			"speed":       math.Round(float64(item.Model.speed)*100) / 100,
			"start_ms":    item.Sub.StartAt.Milliseconds(),
			"end_ms":      item.Sub.EndAt.Milliseconds(),
			"duration_ms": (item.Sub.EndAt - item.Sub.StartAt).Milliseconds(),
			"merged_from": item.MergedFrom,
		}
		cues = append(cues, entry)
	}

	violationEntries := make([]map[string]any, 0, len(violations))
	for _, v := range violations {
		violationEntries = append(violationEntries, map[string]any{
			"index": v.Index,
			"tag":   v.Tag,
			"text":  v.Text,
		})
	}

	cleanupEdits := mechanicalCleanup(texts)
	cleanupEntries := make([]map[string]any, 0, len(cleanupEdits))
	for _, e := range cleanupEdits {
		cleanupEntries = append(cleanupEntries, map[string]any{
			"index":  e.Index,
			"before": e.Before,
			"after":  e.After,
		})
	}

	payload := map[string]any{
		"cues":                     cues,
		"mid_cue_tag_violations":   violationEntries,
		"mechanical_cleanup_edits": cleanupEntries,
	}
	return json.MarshalIndent(payload, "", "  ")
}

func parityFixtureSummary(path string, config *Config, mergeThresholdMs, mergeMaxMs int) ([]byte, error) {
	items := parseSubtitleFile(config, path, mergeThresholdMs, mergeMaxMs)

	subs, err := astisub.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("opening fixture for tag-violation check: %w", err)
	}
	violations := findMidCueTagViolations(subs)

	return paritySummary(items, violations)
}

func runParity(c *console.Context) error {
	fixture := c.Args().Get("fixture")
	if fixture == "" {
		return console.Exit("Missing --fixture path", 1)
	}

	configPath := c.String("config")
	config, err := readConfig(configPath)
	if err != nil {
		return console.Exit(fmt.Sprintf("Error reading config: %v", err), 1)
	}

	threshold := config.MergeLinesThresholdMs
	if c.Int("merge-lines-threshold-ms") > 0 {
		threshold = c.Int("merge-lines-threshold-ms")
	}

	mergeMaxMs := c.Int("merge-max-ms")
	payload, err := parityFixtureSummary(fixture, config, threshold, mergeMaxMs)
	if err != nil {
		return console.Exit(fmt.Sprintf("Error generating parity summary: %v", err), 1)
	}

	outDir := c.String("out-dir")
	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return console.Exit(fmt.Sprintf("Error creating output dir: %v", err), 1)
	}

	outPath := filepath.Join(outDir, "parity-summary.json")
	if err := os.WriteFile(outPath, payload, 0o644); err != nil {
		return console.Exit(fmt.Sprintf("Error writing parity summary: %v", err), 1)
	}

	fmt.Fprintf(c.App.Writer, "%s\n", payload)
	fmt.Fprintf(c.App.Writer, "Wrote %s\n", outPath)
	return nil
}

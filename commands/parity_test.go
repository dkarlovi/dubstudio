package commands

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/asticode/go-astisub"
)

func TestParitySummary_EmitsDeterministicCuePayload(t *testing.T) {
	items := []Item{
		{
			Sub: &astisub.Item{
				Index:   0,
				StartAt: 0,
				EndAt:   2 * time.Second,
				Lines: []astisub.Line{{
					Items: []astisub.LineItem{{Text: "hello there"}},
				}},
			},
			Model:      Model{name: "Matko", model: "voice-m", speed: 1.0},
			MergedFrom: []string{"first", "second"},
		},
		{
			Sub: &astisub.Item{
				Index:   1,
				StartAt: 2 * time.Second,
				EndAt:   4 * time.Second,
				Lines: []astisub.Line{{
					Items: []astisub.LineItem{{Text: "second line"}},
				}},
			},
			Model: Model{name: "Hana", model: "voice-h", speed: 1.15},
		},
	}

	payload, err := paritySummary(items, nil)
	if err != nil {
		t.Fatalf("paritySummary() returned error: %v", err)
	}

	var got struct {
		Cues                   []map[string]any `json:"cues"`
		MidCueTagViolations    []map[string]any `json:"mid_cue_tag_violations"`
		MechanicalCleanupEdits []map[string]any `json:"mechanical_cleanup_edits"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if len(got.Cues) != 2 {
		t.Fatalf("want 2 cues, got %d", len(got.Cues))
	}
	if got.Cues[0]["speaker"] != "Matko" {
		t.Fatalf("first cue speaker = %v, want Matko", got.Cues[0]["speaker"])
	}
	if got.Cues[0]["text"] != "hello there" {
		t.Fatalf("first cue text = %v, want hello there", got.Cues[0]["text"])
	}
	if merged, ok := got.Cues[0]["merged_from"].([]any); !ok || len(merged) != 2 {
		t.Fatalf("first cue merged_from length = %v, want 2", got.Cues[0]["merged_from"])
	}
	if got.Cues[1]["speed"] != 1.15 {
		t.Fatalf("second cue speed = %v, want 1.15", got.Cues[1]["speed"])
	}
	if len(got.MidCueTagViolations) != 0 {
		t.Fatalf("want no violations, got %v", got.MidCueTagViolations)
	}
	if len(got.MechanicalCleanupEdits) != 0 {
		t.Fatalf("want no cleanup edits, got %v", got.MechanicalCleanupEdits)
	}
}

func TestParitySummary_IncludesMechanicalCleanupEdits(t *testing.T) {
	items := []Item{
		{
			Sub: &astisub.Item{
				Index:   0,
				StartAt: 0,
				EndAt:   2 * time.Second,
				Lines: []astisub.Line{{
					Items: []astisub.LineItem{{Text: "So, um, this is, you know, a test."}},
				}},
			},
			Model: Model{name: "Hana", model: "voice-h", speed: 1.0},
		},
	}

	payload, err := paritySummary(items, nil)
	if err != nil {
		t.Fatalf("paritySummary() returned error: %v", err)
	}

	var got struct {
		MechanicalCleanupEdits []map[string]any `json:"mechanical_cleanup_edits"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if len(got.MechanicalCleanupEdits) != 1 {
		t.Fatalf("want 1 edit, got %d: %v", len(got.MechanicalCleanupEdits), got.MechanicalCleanupEdits)
	}
	if got.MechanicalCleanupEdits[0]["after"] != "This is, a test." {
		t.Fatalf("after = %v, want %q", got.MechanicalCleanupEdits[0]["after"], "This is, a test.")
	}
}

func TestParitySummary_IncludesMidCueTagViolations(t *testing.T) {
	violations := []TagViolation{{Index: 2, Tag: "[Matko]", Text: "Hello [Matko] there."}}

	payload, err := paritySummary(nil, violations)
	if err != nil {
		t.Fatalf("paritySummary() returned error: %v", err)
	}

	var got struct {
		MidCueTagViolations []map[string]any `json:"mid_cue_tag_violations"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if len(got.MidCueTagViolations) != 1 {
		t.Fatalf("want 1 violation, got %d", len(got.MidCueTagViolations))
	}
	if got.MidCueTagViolations[0]["tag"] != "[Matko]" {
		t.Fatalf("tag = %v, want [Matko]", got.MidCueTagViolations[0]["tag"])
	}
	if got.MidCueTagViolations[0]["index"] != float64(2) {
		t.Fatalf("index = %v, want 2", got.MidCueTagViolations[0]["index"])
	}
}

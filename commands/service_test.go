package commands

import "testing"

func testServiceConfig() ServiceConfig {
	return ServiceConfig{
		Engine:             uploadTestConfig(),
		OverlapToleranceMs: 0,
		AutoFix:            AutoFixConfig{ToleranceMs: 120, AutofixMarginMs: 40, SpeedCaps: testSpeedCaps},
	}
}

func TestLocalService_Cleanup(t *testing.T) {
	s := &Session{Cues: []*SessionCue{
		{Index: 1, CurrentText: "So, um, this is, you know, a test."},
		{Index: 2, CurrentText: "A perfectly clean sentence."},
	}}
	ls := NewLocalService(testServiceConfig())

	edits := ls.Cleanup(s)

	if len(edits) != 1 || edits[0].Index != 1 {
		t.Fatalf("edits = %+v, want 1 edit on cue 1", edits)
	}
	if s.Cues[0].CurrentText != "This is, a test." {
		t.Errorf("cue 1 current_text = %q", s.Cues[0].CurrentText)
	}
	if s.Cues[1].CurrentText != "A perfectly clean sentence." {
		t.Errorf("cue 2 current_text changed unexpectedly: %q", s.Cues[1].CurrentText)
	}
}

func TestLocalService_AutoFix(t *testing.T) {
	audioMs := 2350
	s := &Session{Cues: []*SessionCue{
		{Index: 1, StartMs: 0, EndMs: 2000, Voice: "hana", Speed: 1.0, AudioMs: &audioMs},
		{Index: 2, StartMs: 2200, EndMs: 4000, Voice: "hana", Speed: 1.0},
	}}
	ls := NewLocalService(testServiceConfig())

	result := ls.AutoFix(s)

	if len(result.Sped) != 1 || result.Sped[0].Index != 1 {
		t.Fatalf("sped = %+v, want cue 1 sped up", result.Sped)
	}
	if s.Cues[0].Speed != 1.09 || !s.Cues[0].SpeedOverride {
		t.Errorf("cue 1 = %+v, want speed 1.09 override", s.Cues[0])
	}
}

func TestLocalService_UpdateCue(t *testing.T) {
	s := &Session{Cues: []*SessionCue{
		{Index: 1, CurrentText: "old", Speed: 1.0, NeedsHuman: true, HumanReason: "was basketed"},
	}}
	ls := NewLocalService(testServiceConfig())

	t.Run("text edit clears the basket flag", func(t *testing.T) {
		newText := "edited"
		got, err := ls.UpdateCue(s, 1, &newText, nil)
		if err != nil {
			t.Fatalf("UpdateCue() error: %v", err)
		}
		if got.CurrentText != "edited" || got.NeedsHuman || got.HumanReason != "" {
			t.Errorf("cue = %+v", got)
		}
	})

	t.Run("speed edit is clamped and marked an override", func(t *testing.T) {
		tooHigh := 5.0
		got, err := ls.UpdateCue(s, 1, nil, &tooHigh)
		if err != nil {
			t.Fatalf("UpdateCue() error: %v", err)
		}
		if got.Speed != float64(MaxSpeed) || !got.SpeedOverride {
			t.Errorf("cue = %+v, want speed clamped to %v and overridden", got, MaxSpeed)
		}
	})

	t.Run("unknown index errors", func(t *testing.T) {
		if _, err := ls.UpdateCue(s, 99, nil, nil); err == nil {
			t.Error("want an error for an unknown cue index")
		}
	})
}

func TestBuildExportResult(t *testing.T) {
	t.Run("an overlap blocks export", func(t *testing.T) {
		_, err := buildExportResult(nil, []cueOverlap{{First: 0, Second: 1}})
		if err == nil {
			t.Fatal("want an error when overlaps remain")
		}
	})

	t.Run("summarizes still-over-budget and basketed cues", func(t *testing.T) {
		overMs := 3000
		underMs := 100
		cues := []*SessionCue{
			{Index: 1, EndMs: 2000, AudioMs: &overMs},
			{Index: 2, EndMs: 2000, AudioMs: &underMs},
			{Index: 3, NeedsHuman: true},
		}
		result, err := buildExportResult(cues, nil)
		if err != nil {
			t.Fatalf("buildExportResult() error: %v", err)
		}
		if len(result.StillOverBudget) != 1 || result.StillOverBudget[0] != 1 {
			t.Errorf("still_over_budget = %v, want [1]", result.StillOverBudget)
		}
		if len(result.Basket) != 1 || result.Basket[0] != 3 {
			t.Errorf("basket = %v, want [3]", result.Basket)
		}
	})
}

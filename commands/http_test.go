package commands

import "testing"

func TestCueDTO(t *testing.T) {
	t.Run("voice is lowercased for the legacy frontend's hardcoded matko/hana check", func(t *testing.T) {
		c := &SessionCue{Voice: "Matko", CurrentText: "hi"}
		got := cueDTO(c, 120)
		if got["voice"] != "matko" {
			t.Errorf("voice = %v, want matko", got["voice"])
		}
	})

	t.Run("flagged uses the given tolerance, not zero", func(t *testing.T) {
		audioMs := 2150 // window 2000ms -> overage 150ms
		c := &SessionCue{StartMs: 0, EndMs: 2000, AudioMs: &audioMs}

		if got := cueDTO(c, 200)["flagged"]; got != false {
			t.Errorf("flagged with tolerance 200 = %v, want false (150ms overage is under tolerance)", got)
		}
		if got := cueDTO(c, 100)["flagged"]; got != true {
			t.Errorf("flagged with tolerance 100 = %v, want true (150ms overage exceeds tolerance)", got)
		}
	})

	t.Run("a cue with no audio yet is never flagged", func(t *testing.T) {
		c := &SessionCue{StartMs: 0, EndMs: 2000}
		if got := cueDTO(c, 0)["flagged"]; got != false {
			t.Errorf("flagged = %v, want false", got)
		}
	})

	t.Run("dirty reflects SessionCue.Dirty()", func(t *testing.T) {
		c := &SessionCue{CurrentText: "hi", LastGeneratedText: "hi", Speed: 1.0, LastGeneratedSpeed: 1.0, HasLastGenerated: true}
		if got := cueDTO(c, 0)["dirty"]; got != false {
			t.Errorf("dirty = %v, want false", got)
		}
		c.CurrentText = "edited"
		if got := cueDTO(c, 0)["dirty"]; got != true {
			t.Errorf("dirty after edit = %v, want true", got)
		}
	})
}

func TestSessionDTO(t *testing.T) {
	t.Run("nil session reports empty", func(t *testing.T) {
		got := sessionDTO(nil, 120, 10)
		if got["empty"] != true {
			t.Errorf("empty = %v, want true", got["empty"])
		}
		if got["mode"] != "real" {
			t.Errorf("mode = %v, want real", got["mode"])
		}
	})

	t.Run("counts and cap/tolerance are surfaced for the frontend's firewall meter", func(t *testing.T) {
		overMs := 3000
		s := &Session{
			Name: "video.vtt", RunCount: 2,
			Cues: []*SessionCue{
				{Index: 1, StartMs: 0, EndMs: 2000, AudioMs: &overMs},
				{Index: 2, NeedsHuman: true},
			},
		}
		got := sessionDTO(s, 120, 10)

		if got["cap"] != 10 || got["tolerance_ms"] != 120 || got["run_count"] != 2 {
			t.Errorf("top-level fields = %+v", got)
		}
		counts := got["counts"].(map[string]any)
		if counts["total"] != 2 || counts["flagged"] != 1 || counts["basket"] != 1 || counts["cleared"] != 1 {
			t.Errorf("counts = %+v", counts)
		}
	})

	t.Run("first_pass_report is passed through for phase detection", func(t *testing.T) {
		s := &Session{Name: "v", FirstPassReport: map[string]any{"cleanup": []CueEdit{}}}
		got := sessionDTO(s, 0, 10)
		fpr, ok := got["first_pass_report"].(map[string]any)
		if !ok {
			t.Fatalf("first_pass_report = %v, want a map", got["first_pass_report"])
		}
		if _, ok := fpr["cleanup"]; !ok {
			t.Errorf("first_pass_report missing cleanup key: %+v", fpr)
		}
	})
}

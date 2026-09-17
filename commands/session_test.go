package commands

import "testing"

func TestSession_Flagged(t *testing.T) {
	t.Run("a skipped cue is excluded even if genuinely over budget", func(t *testing.T) {
		overMs := 3000
		s := &Session{Cues: []*SessionCue{
			{Index: 1, StartMs: 0, EndMs: 2000, AudioMs: &overMs, Skipped: true},
			{Index: 2, StartMs: 0, EndMs: 2000, AudioMs: &overMs},
		}}
		got := s.Flagged(0)
		if len(got) != 1 || got[0].Index != 2 {
			t.Fatalf("Flagged() = %+v, want only cue 2", got)
		}
	})
}

func TestSession_Basket(t *testing.T) {
	t.Run("a skipped cue is excluded even if AutoFix marked it needs_human", func(t *testing.T) {
		s := &Session{Cues: []*SessionCue{
			{Index: 1, NeedsHuman: true, Skipped: true},
			{Index: 2, NeedsHuman: true},
		}}
		got := s.Basket()
		if len(got) != 1 || got[0].Index != 2 {
			t.Fatalf("Basket() = %+v, want only cue 2", got)
		}
	})
}

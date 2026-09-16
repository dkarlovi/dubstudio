package commands

import "testing"

// Mirrors dub-studio's tests/test_vtt_and_cleanup.py::TestMechanicalCleanup
// (mechanical_cleanup/_clean_text in core.py) -- deterministic, free,
// no-synthesis filler/hedge removal.
func TestCleanText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"strips filler words and hedge phrases",
			"So, um, this is, you know, a test.",
			"This is, a test.",
		},
		{
			"leaves clean text untouched",
			"A perfectly clean sentence.",
			"A perfectly clean sentence.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanText(tc.in); got != tc.want {
				t.Errorf("cleanText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMechanicalCleanup(t *testing.T) {
	t.Run("reports edited cues", func(t *testing.T) {
		got := mechanicalCleanup([]string{"So, um, this is, you know, a test."})
		if len(got) != 1 {
			t.Fatalf("want 1 edit, got %d: %v", len(got), got)
		}
		if got[0].Index != 1 {
			t.Errorf("index = %d, want 1", got[0].Index)
		}
		if got[0].After != "This is, a test." {
			t.Errorf("after = %q, want %q", got[0].After, "This is, a test.")
		}
	})

	t.Run("reports nothing for already-clean text", func(t *testing.T) {
		got := mechanicalCleanup([]string{"A perfectly clean sentence."})
		if len(got) != 0 {
			t.Fatalf("want no edits, got %v", got)
		}
	})
}

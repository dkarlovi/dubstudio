package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCanMergeCue(t *testing.T) {
	cases := []struct {
		name                                       string
		curSpeaker, nextSpeaker                    string
		mergedStart, mergedEnd, nextStart, nextEnd time.Duration
		mergeThresholdMs, mergeMaxMs               int
		want                                       bool
	}{
		{
			"same voice, within gap, within cap",
			"hana", "hana", 0, 3 * time.Second, 3 * time.Second, 6 * time.Second,
			120, 7000, true,
		},
		{
			"same voice, within gap, cap 0 means unlimited",
			"hana", "hana", 0, 3 * time.Second, 3 * time.Second, 10 * time.Second,
			120, 0, true,
		},
		{
			"same voice, within gap, but candidate window exceeds cap",
			"hana", "hana", 0, 6 * time.Second, 6 * time.Second, 9 * time.Second,
			120, 7000, false,
		},
		{
			"different voice never merges, even with room under the cap",
			"hana", "matko", 0, 3 * time.Second, 3 * time.Second, 4 * time.Second,
			120, 7000, false,
		},
		{
			"gap past threshold still blocks merging",
			"hana", "hana", 0, 3 * time.Second, 3*time.Second + 200*time.Millisecond, 6 * time.Second,
			120, 7000, false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := canMergeCue(tc.curSpeaker, tc.nextSpeaker, tc.mergedStart, tc.mergedEnd, tc.nextStart, tc.nextEnd, tc.mergeThresholdMs, tc.mergeMaxMs)
			if got != tc.want {
				t.Errorf("canMergeCue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func writeTempVTT(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.vtt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp VTT: %v", err)
	}
	return path
}

// threeAbuttingSameVoiceCues are three 3s Matko cues, back to back with no
// gap. Each line is explicitly tagged: an untagged line resolves to the
// default speaker (Hana in testConfig), not "whichever speaker tagged the
// previous line".
const threeAbuttingSameVoiceCues = `WEBVTT

00:00:00.000 --> 00:00:03.000
[Matko] one

00:00:03.000 --> 00:00:06.000
[Matko] two

00:00:06.000 --> 00:00:09.000
[Matko] three
`

func TestParseSubtitleFile_MergeMaxMs(t *testing.T) {
	cfg := testConfig()

	t.Run("cap 7000ms splits into [1+2],[3]", func(t *testing.T) {
		path := writeTempVTT(t, threeAbuttingSameVoiceCues)
		items := parseSubtitleFile(cfg, path, 120, 7000)

		if len(items) != 2 {
			t.Fatalf("want 2 merged cues, got %d: %+v", len(items), items)
		}
		if len(items[0].MergedFrom) != 2 {
			t.Errorf("want first cue merged from 2 lines, got %d", len(items[0].MergedFrom))
		}
		if len(items[1].MergedFrom) != 1 {
			t.Errorf("want second cue merged from 1 line, got %d", len(items[1].MergedFrom))
		}
	})

	t.Run("cap 0 is unlimited: all three merge", func(t *testing.T) {
		path := writeTempVTT(t, threeAbuttingSameVoiceCues)
		items := parseSubtitleFile(cfg, path, 120, 0)

		if len(items) != 1 {
			t.Fatalf("want 1 merged cue, got %d: %+v", len(items), items)
		}
		if len(items[0].MergedFrom) != 3 {
			t.Errorf("want cue merged from 3 lines, got %d", len(items[0].MergedFrom))
		}
	})

	t.Run("a different-voice cue in between always breaks the merge, cap or no cap", func(t *testing.T) {
		// Hana is the default speaker in testConfig, so her lines are
		// untagged; Matko's line in between carries the explicit tag.
		vtt := `WEBVTT

00:00:00.000 --> 00:00:03.000
one

00:00:03.000 --> 00:00:06.000
[Matko] two

00:00:06.000 --> 00:00:09.000
three
`
		path := writeTempVTT(t, vtt)
		items := parseSubtitleFile(cfg, path, 120, 0)

		if len(items) != 3 {
			t.Fatalf("want 3 separate cues (Hana/Matko/Hana never merges across Matko), got %d: %+v", len(items), items)
		}
		for _, item := range items {
			if len(item.MergedFrom) != 1 {
				t.Errorf("want no merging across voices, got MergedFrom=%d for %q", len(item.MergedFrom), item.Sub.String())
			}
		}
	})
}

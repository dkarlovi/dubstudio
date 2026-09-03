package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asticode/go-astisub"
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

func writeFileWithModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestNewestFile(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "older.mp3")
	newer := filepath.Join(dir, "newer.mp3")
	now := time.Now()
	writeFileWithModTime(t, older, now.Add(-1*time.Hour))
	writeFileWithModTime(t, newer, now)

	t.Run("older listed first", func(t *testing.T) {
		got, err := newestFile([]string{older, newer})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != newer {
			t.Errorf("want %s, got %s", newer, got)
		}
	})

	t.Run("newer listed first -- order must not matter", func(t *testing.T) {
		got, err := newestFile([]string{newer, older})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != newer {
			t.Errorf("want %s, got %s", newer, got)
		}
	})

	t.Run("single file", func(t *testing.T) {
		got, err := newestFile([]string{newer})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != newer {
			t.Errorf("want %s, got %s", newer, got)
		}
	})

	t.Run("missing file errors instead of picking blindly", func(t *testing.T) {
		if _, err := newestFile([]string{filepath.Join(dir, "does-not-exist.mp3")}); err == nil {
			t.Fatal("want an error for a missing file, got nil")
		}
	})
}

func TestGeneratePathTemplate_PrefersNewestOnDuplicateCacheMatch(t *testing.T) {
	dir := t.TempDir()
	item := &astisub.Item{
		Lines: []astisub.Line{{Items: []astisub.LineItem{{Text: "hello there"}}}},
	}
	model := Model{model: "voice-id", name: "Matko", ttsModel: "eleven_multilingual_v2", speed: 1.0}

	// No cache files yet -- confirms the template and lets the test build
	// matching filenames without hand-computing the checksum.
	first := generatePathTemplate(dir, item, model)
	if first.Path != "" {
		t.Fatalf("want no cache hit yet, got %+v", first)
	}

	olderPath := fmt.Sprintf(first.Template, "older-id")
	newerPath := fmt.Sprintf(first.Template, "newer-id")
	now := time.Now()
	writeFileWithModTime(t, olderPath, now.Add(-1*time.Hour))
	writeFileWithModTime(t, newerPath, now)

	got := generatePathTemplate(dir, item, model)
	if got.Id != "newer-id" {
		t.Errorf("want the newest cache file's id %q, got %q (path %s)", "newer-id", got.Id, got.Path)
	}
}

func TestGeneratePathTemplate_SingleCacheMatchUnaffected(t *testing.T) {
	dir := t.TempDir()
	item := &astisub.Item{
		Lines: []astisub.Line{{Items: []astisub.LineItem{{Text: "only one take"}}}},
	}
	model := Model{model: "voice-id", name: "Hana", ttsModel: "eleven_multilingual_v2", speed: 1.2}

	first := generatePathTemplate(dir, item, model)
	onlyPath := fmt.Sprintf(first.Template, "only-id")
	writeFileWithModTime(t, onlyPath, time.Now())

	got := generatePathTemplate(dir, item, model)
	if got.Id != "only-id" {
		t.Errorf("want %q, got %q", "only-id", got.Id)
	}
}

package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/asticode/go-astisub"
)

func uploadTestConfig() *Config {
	return &Config{
		Default: SpeakerConfig{Model: "hana_id", Name: "Hana", Speed: 1.0},
		Models: map[string]SpeakerConfig{
			"Matko": {Model: "matko_id", Name: "Matko", Speed: 1.0},
		},
	}
}

// Mirrors dub-studio's tests/test_vtt_and_cleanup.py::TestParseVtt, general
// to configured speaker names instead of Python's hardcoded matko/hana.
func TestSessionCuesFromSubtitles(t *testing.T) {
	t.Run("leading tag sets voice and is stripped", func(t *testing.T) {
		subs, err := astisub.ReadFromWebVTT(strings.NewReader(
			"WEBVTT\n\n" +
				"00:00:00.000 --> 00:00:02.000\n[Matko] Hello there.\n\n" +
				"00:00:02.000 --> 00:00:04.000\nHi Hana here, no tag.\n"))
		if err != nil {
			t.Fatalf("ReadFromWebVTT() error: %v", err)
		}
		cues, err := sessionCuesFromSubtitles(subs, uploadTestConfig())
		if err != nil {
			t.Fatalf("sessionCuesFromSubtitles() error: %v", err)
		}
		if len(cues) != 2 {
			t.Fatalf("want 2 cues, got %d", len(cues))
		}
		if cues[0].Voice != "Matko" {
			t.Errorf("cue 1 voice = %q, want Matko", cues[0].Voice)
		}
		if cues[0].SourceText != "Hello there." {
			t.Errorf("cue 1 source_text = %q, want %q", cues[0].SourceText, "Hello there.")
		}
		if cues[0].StartMs != 0 || cues[0].EndMs != 2000 {
			t.Errorf("cue 1 timing = %d-%d, want 0-2000", cues[0].StartMs, cues[0].EndMs)
		}
		if cues[1].Voice != "Hana" {
			t.Errorf("cue 2 voice = %q, want Hana", cues[1].Voice)
		}
		if cues[1].SourceText != "Hi Hana here, no tag." {
			t.Errorf("cue 2 source_text = %q", cues[1].SourceText)
		}
	})

	t.Run("untagged cue defaults to the configured default speaker", func(t *testing.T) {
		subs, err := astisub.ReadFromWebVTT(strings.NewReader(
			"WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nJust a line.\n"))
		if err != nil {
			t.Fatalf("ReadFromWebVTT() error: %v", err)
		}
		cues, err := sessionCuesFromSubtitles(subs, uploadTestConfig())
		if err != nil {
			t.Fatalf("sessionCuesFromSubtitles() error: %v", err)
		}
		if cues[0].Voice != "Hana" {
			t.Errorf("voice = %q, want Hana", cues[0].Voice)
		}
	})

	t.Run("an authored per-line speed override is honored, unlike the PoC's simplified parser", func(t *testing.T) {
		subs, err := astisub.ReadFromWebVTT(strings.NewReader(
			"WEBVTT\n\n00:00:00.000 --> 00:00:02.000\n[Matko@1.15] Hello.\n"))
		if err != nil {
			t.Fatalf("ReadFromWebVTT() error: %v", err)
		}
		cues, err := sessionCuesFromSubtitles(subs, uploadTestConfig())
		if err != nil {
			t.Fatalf("sessionCuesFromSubtitles() error: %v", err)
		}
		if !cues[0].SpeedOverride || cues[0].Speed != 1.15 {
			t.Errorf("cue 1 = %+v, want speed 1.15 override", cues[0])
		}
	})
}

func TestNewSessionFromSubtitleFile_RejectsMidCueTagViolations(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/fixture.vtt"
	if err := os.WriteFile(path, []byte("WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nHello [Matko] there.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	session, violations, err := NewSessionFromSubtitleFile("t", path, uploadTestConfig())
	if err != nil {
		t.Fatalf("NewSessionFromSubtitleFile() error: %v", err)
	}
	if session != nil {
		t.Fatalf("want no session built when violations exist, got %+v", session)
	}
	if len(violations) != 1 {
		t.Fatalf("want 1 violation, got %v", violations)
	}
}

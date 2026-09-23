package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleIndex(t *testing.T) {
	t.Run("serves the embedded frontend by default, no static-dir needed", func(t *testing.T) {
		h := newHTTPServer(localTenantFunc(ServiceConfig{WorkDir: t.TempDir()}), "", 0, 0)

		w := httptest.NewRecorder()
		h.mux().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if !strings.Contains(w.Body.String(), "<title>Dub Studio</title>") {
			t.Errorf("body doesn't look like the embedded frontend: %.100s...", w.Body.String())
		}
	})

	t.Run("static-dir overrides the embedded default when set", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>Local Dev Copy</title>"), 0o644); err != nil {
			t.Fatal(err)
		}
		h := newHTTPServer(localTenantFunc(ServiceConfig{WorkDir: t.TempDir()}), dir, 0, 0)

		w := httptest.NewRecorder()
		h.mux().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		if !strings.Contains(w.Body.String(), "Local Dev Copy") {
			t.Errorf("body = %q, want the static-dir override served instead of the embedded default", w.Body.String())
		}
	})
}

func TestCueDTO(t *testing.T) {
	t.Run("voice is lowercased for the legacy frontend's hardcoded matko/hana check", func(t *testing.T) {
		c := &SessionCue{Voice: "Matko", CurrentText: "hi"}
		got := cueDTO(c)
		if got["voice"] != "matko" {
			t.Errorf("voice = %v, want matko", got["voice"])
		}
	})

	t.Run("flagged reflects needs_human, not raw window overage", func(t *testing.T) {
		// found live 2026-09-18: a cue can run well past its own subtitle
		// window and still be entirely fine, because AutoFix's real budget
		// (time until the same voice speaks again) is deliberately more
		// generous than that window. Using raw overage here buried the
		// cues that actually needed attention in noise.
		audioMs := 5000 // window 2000ms -> overage 3000ms, but AutoFix decided this is fine
		c := &SessionCue{StartMs: 0, EndMs: 2000, AudioMs: &audioMs, NeedsHuman: false}
		if got := cueDTO(c)["flagged"]; got != false {
			t.Errorf("flagged = %v, want false (large raw overage, but AutoFix didn't basket it)", got)
		}

		c.NeedsHuman = true
		if got := cueDTO(c)["flagged"]; got != true {
			t.Errorf("flagged = %v, want true once AutoFix baskets it", got)
		}
	})

	t.Run("a skipped cue is never flagged even if needs_human", func(t *testing.T) {
		audioMs := 5000
		c := &SessionCue{StartMs: 0, EndMs: 2000, AudioMs: &audioMs, NeedsHuman: true, Skipped: true}
		if got := cueDTO(c)["flagged"]; got != false {
			t.Errorf("flagged = %v, want false (skipped overrides needs_human)", got)
		}
	})

	t.Run("a cue with no audio yet is never flagged", func(t *testing.T) {
		c := &SessionCue{StartMs: 0, EndMs: 2000}
		if got := cueDTO(c)["flagged"]; got != false {
			t.Errorf("flagged = %v, want false", got)
		}
	})

	t.Run("dirty reflects SessionCue.Dirty()", func(t *testing.T) {
		c := &SessionCue{CurrentText: "hi", LastGeneratedText: "hi", Speed: 1.0, LastGeneratedSpeed: 1.0, HasLastGenerated: true}
		if got := cueDTO(c)["dirty"]; got != false {
			t.Errorf("dirty = %v, want false", got)
		}
		c.CurrentText = "edited"
		if got := cueDTO(c)["dirty"]; got != true {
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
		fineOverage := 3000 // over its own window, but AutoFix left it alone -- not flagged, see cueDTO
		basketedMs := 5000
		s := &Session{
			Name: "video.vtt", RunCount: 2,
			Cues: []*SessionCue{
				{Index: 1, StartMs: 0, EndMs: 2000, AudioMs: &fineOverage},
				{Index: 2, StartMs: 0, EndMs: 2000, AudioMs: &basketedMs, NeedsHuman: true},
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

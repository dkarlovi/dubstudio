package commands

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLog redirects the standard logger for the duration of a test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	flags, w := log.Flags(), log.Writer()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(w)
		log.SetFlags(flags)
	})
	return &buf
}

func TestLoggingMiddleware(t *testing.T) {
	t.Run("logs method, path, status and size", func(t *testing.T) {
		buf := captureLog(t)
		h := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			w.Write([]byte("hello"))
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/generate", nil))

		got := buf.String()
		for _, want := range []string{"POST", "/generate", "418", "5 bytes"} {
			if !strings.Contains(got, want) {
				t.Errorf("log line %q is missing %q", strings.TrimSpace(got), want)
			}
		}
	})

	t.Run("a handler that never calls WriteHeader is logged as 200", func(t *testing.T) {
		buf := captureLog(t)
		h := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok"))
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/session", nil))

		if got := buf.String(); !strings.Contains(got, "-> 200") {
			t.Errorf("log line %q, want an implicit 200", strings.TrimSpace(got))
		}
	})

	t.Run("the first status wins, as it does on the wire", func(t *testing.T) {
		buf := captureLog(t)
		h := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			w.WriteHeader(http.StatusOK) // net/http ignores this too
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/export", nil))

		got := buf.String()
		if !strings.Contains(got, "-> 502") {
			t.Errorf("log line %q, want the first status (502)", strings.TrimSpace(got))
		}
	})

	t.Run("the response still reaches the client unchanged", func(t *testing.T) {
		captureLog(t)
		h := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"ok":true}`))
		}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/upload", nil))

		if w.Code != http.StatusCreated {
			t.Errorf("status = %d, want 201", w.Code)
		}
		if got := w.Body.String(); got != `{"ok":true}` {
			t.Errorf("body = %q, want it passed through untouched", got)
		}
		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want it passed through", got)
		}
	})
}

func TestRequestSessionLabel(t *testing.T) {
	const id = "7e1c067fa8fd35a167409883dc40c7b2"

	t.Run("logs only a prefix, never the whole cookie", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/session", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: id})

		got := requestSessionLabel(r)
		if got != id[:8] {
			t.Errorf("label = %q, want %q", got, id[:8])
		}
		// The cookie is a credential: a log holding it in full hands over
		// every session that appears there.
		if strings.Contains(got, id) || len(got) >= len(id) {
			t.Errorf("label %q leaks the full session id", got)
		}
	})

	t.Run("no cookie, or one that isn't a session id, logs as -", func(t *testing.T) {
		for _, tc := range []struct{ name, cookie string }{
			{"absent", ""},
			{"empty", ""},
			{"traversal attempt", "../../../../etc/passwd"},
			{"truncated", id[:16]},
		} {
			r := httptest.NewRequest(http.MethodGet, "/session", nil)
			if tc.cookie != "" {
				r.Header.Set("Cookie", sessionCookieName+"="+tc.cookie)
			}
			if got := requestSessionLabel(r); got != "-" {
				t.Errorf("%s: label = %q, want -", tc.name, got)
			}
		}
	})
}

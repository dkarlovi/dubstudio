package commands

import (
	"log"
	"net/http"
	"time"
)

// statusRecorder captures what was actually written, since net/http gives
// a middleware no way to read the status code back off a ResponseWriter
// after the handler has run.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	bytes   int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.written {
		s.status = code
		s.written = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	// A handler that writes without calling WriteHeader has implicitly
	// sent 200, which is what status was seeded with.
	s.written = true
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// loggingMiddleware logs one line per request: method, path, the calling
// session, status, response size and duration.
//
// serve had no request log at all, so a failing frontend call could not be
// placed against what the server actually did -- and with per-session
// state that got worse, since two browsers' requests are interleaved in
// one process with nothing to tell them apart.
//
// Only the first 8 characters of the session id are logged. The full value
// is the cookie, i.e. a credential: anyone holding it is that session, so
// writing it to a log file in full would hand over whatever sessions
// appear there. 8 hex characters are plenty to correlate requests within
// one process's lifetime. An absent or malformed cookie logs "-" rather
// than being invented, since resolveTenant will issue a fresh id further
// in, and that request genuinely had no session yet.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		log.Printf("%s %s session=%s -> %d (%d bytes) in %s",
			r.Method, r.URL.Path, requestSessionLabel(r), rec.status, rec.bytes,
			time.Since(started).Round(time.Millisecond))
	})
}

// requestSessionLabel is the short, non-credential session identifier used
// in the request log. See loggingMiddleware on why it is truncated.
func requestSessionLabel(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || !validSessionID(c.Value) {
		return "-"
	}
	return c.Value[:8]
}

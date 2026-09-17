package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/symfony-cli/console"
)

// httpServer is the HTTP transport for the Service/SessionStore contract:
// it depends only on those interfaces, not on LocalService/FileSessionStore
// concretely, so a different implementation of either can be swapped in
// without this file changing. Mirrors dub-studio's app.py route-for-route
// (see that file's own docstring), now including /reduce.
//
// Like app.py, this holds a single global session in memory (not
// multi-tenant), persisted through store after every mutating call.
type httpServer struct {
	mu      sync.Mutex
	session *Session

	service     Service
	store       SessionStore
	staticDir   string
	runCap      int
	toleranceMs int
}

func newHTTPServer(service Service, store SessionStore, staticDir string, runCap, toleranceMs int) (*httpServer, error) {
	session, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("loading session: %w", err)
	}
	return &httpServer{
		session:     session,
		service:     service,
		store:       store,
		staticDir:   staticDir,
		runCap:      runCap,
		toleranceMs: toleranceMs,
	}, nil
}

func (h *httpServer) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", h.handleGetSession)
	mux.HandleFunc("POST /upload", h.handleUpload)
	mux.HandleFunc("POST /cleanup", h.handleCleanup)
	mux.HandleFunc("POST /generate", h.handleGenerate)
	mux.HandleFunc("POST /autofix", h.handleAutoFix)
	mux.HandleFunc("POST /reduce", h.handleReduce)
	mux.HandleFunc("POST /cue/{index}", h.handleUpdateCue)
	mux.HandleFunc("POST /export", h.handleExport)
	mux.HandleFunc("GET /export/download", h.handleExportDownload)
	mux.HandleFunc("POST /reset", h.handleReset)

	if h.staticDir != "" {
		mux.HandleFunc("GET /{$}", h.handleIndex)
		mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(h.staticDir))))
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"detail": message})
}

func (h *httpServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(h.staticDir, "index.html"))
}

func (h *httpServer) handleGetSession(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	writeJSON(w, http.StatusOK, sessionDTO(h.session, h.toleranceMs, h.runCap))
}

func (h *httpServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "no file uploaded: "+err.Error())
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "dubstudio-upload-*.vtt")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp.Close()

	session, violations, err := h.service.Upload(header.Filename, tmp.Name())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(violations) > 0 {
		var detail strings.Builder
		for i, v := range violations {
			if i > 0 {
				detail.WriteString("; ")
			}
			fmt.Fprintf(&detail, "cue %d: tag %s is not at the start of the line", v.Index, v.Tag)
		}
		writeError(w, http.StatusBadRequest,
			"Speaker tags must be at the very start of a cue. "+detail.String())
		return
	}

	h.session = session
	if !h.save(w) {
		return
	}
	writeJSON(w, http.StatusOK, sessionDTO(h.session, h.toleranceMs, h.runCap))
}

// save persists the current session, writing a 500 and returning false on
// failure so callers can bail out of their handler.
func (h *httpServer) save(w http.ResponseWriter) bool {
	if err := h.store.Save(h.session); err != nil {
		writeError(w, http.StatusInternalServerError, "saving session: "+err.Error())
		return false
	}
	return true
}

func (h *httpServer) requireSession(w http.ResponseWriter) bool {
	if h.session == nil {
		writeError(w, http.StatusBadRequest, "No active session.")
		return false
	}
	return true
}

func (h *httpServer) handleCleanup(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.requireSession(w) {
		return
	}

	edits := h.service.Cleanup(h.session)
	if !h.save(w) {
		return
	}
	out := sessionDTO(h.session, h.toleranceMs, h.runCap)
	out["last_cleanup"] = map[string]any{"cleanup": edits}
	writeJSON(w, http.StatusOK, out)
}

func (h *httpServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.requireSession(w) {
		return
	}

	result, err := h.service.Generate(h.session)
	if err != nil {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if !h.save(w) {
		return
	}
	out := sessionDTO(h.session, h.toleranceMs, h.runCap)
	out["last_run"] = result
	writeJSON(w, http.StatusOK, out)
}

func (h *httpServer) handleAutoFix(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.requireSession(w) {
		return
	}

	result := h.service.AutoFix(h.session)
	if !h.save(w) {
		return
	}
	out := sessionDTO(h.session, h.toleranceMs, h.runCap)
	out["last_autofix"] = result
	writeJSON(w, http.StatusOK, out)
}

func (h *httpServer) handleReduce(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.requireSession(w) {
		return
	}

	result, err := h.service.Reduce(h.session)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.save(w) {
		return
	}
	out := sessionDTO(h.session, h.toleranceMs, h.runCap)
	out["last_reduce"] = result
	writeJSON(w, http.StatusOK, out)
}

func (h *httpServer) handleExport(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.requireSession(w) {
		return
	}

	stillOver := h.session.Flagged(h.toleranceMs)
	basket := h.session.Basket()

	result, err := h.service.Export(h.session)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if !h.save(w) {
		return
	}

	manifest := map[string]any{
		"video":             h.session.Name,
		"cue_count":         len(h.session.Cues),
		"still_over_budget": indices(stillOver),
		"basket":            indices(basket),
		"wav_path":          result.WavPath,
	}
	out := sessionDTO(h.session, h.toleranceMs, h.runCap)
	out["last_export"] = manifest
	writeJSON(w, http.StatusOK, out)
}

func (h *httpServer) handleExportDownload(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	session := h.session
	h.mu.Unlock()

	if session == nil || session.ExportWavPath == "" {
		writeError(w, http.StatusNotFound, "No exported audio yet.")
		return
	}
	if _, err := os.Stat(session.ExportWavPath); err != nil {
		writeError(w, http.StatusNotFound, "Exported file is no longer on disk.")
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(session.ExportWavPath)))
	http.ServeFile(w, r, session.ExportWavPath)
}

func (h *httpServer) handleUpdateCue(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.requireSession(w) {
		return
	}

	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cue index")
		return
	}

	var payload struct {
		Text  *string  `json:"text"`
		Speed *float64 `json:"speed"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
	}

	cue, err := h.service.UpdateCue(h.session, index, payload.Text, payload.Speed)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("No cue #%d.", index))
		return
	}
	if !h.save(w) {
		return
	}
	writeJSON(w, http.StatusOK, cueDTO(cue, h.toleranceMs))
}

func (h *httpServer) handleReset(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.session = nil
	if err := h.store.Reset(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func indices(cues []*SessionCue) []int {
	out := make([]int, 0, len(cues))
	for _, c := range cues {
		out = append(out, c.Index)
	}
	return out
}

// cueDTO adapts the general SessionCue model to the exact shape dub-
// studio's existing frontend (static/index.html) expects: a lowercase
// voice name (that UI is hardcoded to the matko/hana pair), and derived
// flagged/est_flagged/dirty fields dub-studio's own core.Cue exposes as
// properties. est_flagged/est_overage_ms are always zero-valued -- this Go
// service has no mock/estimate mode.
func cueDTO(c *SessionCue, toleranceMs int) map[string]any {
	overageMs := c.OverageMs()
	flagged := c.AudioMs != nil && overageMs > toleranceMs
	return map[string]any{
		"index":           c.Index,
		"start_ms":        c.StartMs,
		"end_ms":          c.EndMs,
		"voice":           strings.ToLower(c.Voice),
		"source_text":     c.SourceText,
		"current_text":    c.CurrentText,
		"speed":           c.Speed,
		"speed_override":  c.SpeedOverride,
		"audio_ms":        c.AudioMs,
		"cache_path":      c.CachePath,
		"overlap_flagged": c.OverlapFlagged,
		"needs_human":     c.NeedsHuman,
		"human_reason":    c.HumanReason,
		"ai_suggestion":   c.AiSuggestion,
		"ai_note":         c.AiNote,
		"window_ms":       c.WindowMs(),
		"overage_ms":      overageMs,
		"est_overage_ms":  0,
		"flagged":         flagged,
		"est_flagged":     false,
		"dirty":           c.Dirty(),
	}
}

func sessionDTO(s *Session, toleranceMs, runCap int) map[string]any {
	if s == nil {
		return map[string]any{"empty": true, "mode": "real"}
	}

	cues := make([]map[string]any, 0, len(s.Cues))
	flaggedCount, basketCount := 0, 0
	for _, c := range s.Cues {
		d := cueDTO(c, toleranceMs)
		cues = append(cues, d)
		if d["flagged"].(bool) {
			flaggedCount++
		}
		if c.NeedsHuman {
			basketCount++
		}
	}

	return map[string]any{
		"name":            s.Name,
		"run_count":       s.RunCount,
		"cap":             runCap,
		"tolerance_ms":    toleranceMs,
		"mode":            "real",
		"generated":       s.Generated(),
		"export_wav_path": s.ExportWavPath,
		"counts": map[string]any{
			"total":       len(s.Cues),
			"flagged":     flaggedCount,
			"est_flagged": 0,
			"basket":      basketCount,
			"cleared":     len(s.Cues) - flaggedCount,
		},
		"first_pass_report": s.FirstPassReport,
		"cues":              cues,
	}
}

func runServe(c *console.Context) error {
	engine, err := readConfig(c.String("config"))
	if err != nil {
		return console.Exit(fmt.Sprintf("Error reading config: %v", err), 1)
	}

	afCfg := builtinAutoFixConfig()
	if path := c.String("autofix-config"); path != "" {
		afCfg, err = loadAutoFixConfigFile(path)
		if err != nil {
			return console.Exit(fmt.Sprintf("Error reading autofix config: %v", err), 1)
		}
	}

	svcCfg := sessionServiceConfig(c, engine)
	svcCfg.AutoFix = afCfg
	service := NewLocalService(svcCfg)
	store := sessionStore(c)

	server, err := newHTTPServer(service, store, c.String("static-dir"), c.Int("run-cap"), c.Int("overlap-tolerance-ms"))
	if err != nil {
		return console.Exit(fmt.Sprintf("Error starting server: %v", err), 1)
	}

	addr := c.String("addr")
	fmt.Fprintf(c.App.Writer, "Listening on %s (work-dir=%s)\n", addr, svcCfg.WorkDir)
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.mux(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		return console.Exit(fmt.Sprintf("Server error: %v", err), 1)
	}
	return nil
}

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`dubstudio` is a Go CLI, built around an engine that converts a subtitle file (`.srt` or `.vtt`) into a WAV audio track using ElevenLabs Text-to-Speech, matching the subtitle timings — for replacing a video's audio track using only its subtitles as the script. The base engine is exposed directly via the `run` command.

On top of that engine, this repo also has an app-level layer (`session-*` CLI commands and a `serve` HTTP API with an embedded frontend) that ports the business logic and orchestration of a separate side project, Dub Studio — see "Session/app layer" below. That layer is additive and app-specific (hardcoded two-speaker UI, opinionated defaults); it's never required to use the engine standalone via `run`.

## Commands

```sh
go build -o dubstudio .          # build (first build ~16s, cached ~0.5s)
go vet ./...                     # static analysis
gofmt -l .                       # formatting check (empty output = clean)
go test ./...                    # run all tests
go test ./... -v                 # verbose, per-subtest output
go test ./commands/ -run TestCanMergeCue        # a single test
go test ./commands/ -run TestCanMergeCue/cap_0  # a single subtest (t.Run name, spaces -> underscores)
```

Run the full `build/vet/test/gofmt` sequence after every change — CI (`.github/workflows/build.yaml`) runs `go test -v ./...` and builds release binaries for Linux/macOS/Windows via GoReleaser on tag pushes.

Manual smoke test (no real ElevenLabs key needed to reach the parsing/config stages):

```sh
cp config.yaml.dist config.yaml   # then fill in a real or dummy auth_key
./dubstudio run data/some_file.vtt
```

It will fail at the ElevenLabs API call with a dummy key — that's expected. A failure before that point (config parsing, subtitle parsing) is a real bug. The same applies to the session/`serve` layer: `./dubstudio serve --work-dir /tmp/x` needs zero flags beyond `--config` to serve a working UI (the frontend is embedded via `go:embed`), and will only fail once a step actually calls ElevenLabs or Anthropic.

## Architecture

Everything lives in the `commands` package; `main.go` just wires `commands.All()` into a `symfony-cli/console` app with a global `-c`/`--config` flag.

### Engine (the `run` command)

`commands/run.go`, `commands/speaker.go`, `commands/overlap.go`, plus `_test.go` siblings. `run` (`commands/run.go:Run`) is a five-stage pipeline:

1. **Parse** (`parseSubtitleFile`) — reads the subtitle file via `go-astisub`, resolves each cue's speaker and optional per-line speed override, and optionally merges adjacent same-speaker cues.
2. **Resolve speaker/speed per cue** (`commands/speaker.go`) — a speaker can be set three mutually-exclusive ways: a VTT `<v Name>` tag, a `NOTE Name` comment, or a leading `[Name]` bracket tag (`resolveSpeaker`, front-anchored regex — a tag anywhere but the very start of the cue is _not_ recognized as a speaker switch, and is sent to TTS as literal text). Any of these three forms can carry `@speed` (e.g. `[Matko@1.15]`, `[@1.15]` for the default speaker) via `splitSpeakerSpec`, overriding that speaker's configured speed for just that line. Speed is clamped to `[MinSpeed, MaxSpeed]` = `[0.7, 1.2]` and rejected outside that range at parse time, not clamped silently.
3. **Merge** (`canMergeCue`, inside `parseSubtitleFile`'s loop) — a pure decision function: same voice, gap under `--merge-lines-threshold-ms`/`-m`, and the merged group's total window under `--merge-max-ms` (0 = unlimited). A merged-in line's own leading speaker tag is stripped _before_ folding its text in (`resolveSpeaker` only strips a tag anchored at the string's start, so this must happen per-line during the merge, not once at the end — see the comment at the strip call site if touching this).
4. **Generate** (`generateMissingVoiceLines`) — for each cue, skips synthesis if a cached mp3 already exists (see caching below); otherwise calls ElevenLabs with [request stitching](https://elevenlabs.io/docs/eleven-api/guides/how-to/text-to-speech/request-stitching) context from `previousIdsFor` (nearest preceding cues by position, regardless of speaker/speed — this used to skip speed-overridden cues and break stitching continuity, see `commands/run_test.go:TestPreviousIdsFor`) and the next few cues' IDs/text, for cross-line continuity.
5. **Check overlaps, then mix** (`findOverlaps`/`findCrossOverlaps` in `commands/overlap.go`, `generateFinalAudioFile`) — same-voice overlap is a genuine collision (each voice renders to its own output channel) and is fatal past `--overlap-tolerance-ms`/`-t` (default 0); cross-voice overlap is always reported (`(CROSS-OVERLAP Nms)`) but only fails the run if `--cross-overlap-tolerance-ms` is explicitly set (default -1 = report-only). Final mixing decodes each cue's PCM, optionally normalizes it toward `--normalize-target-db` RMS (`normalizationGain`, capped at +24dB boost and a -1dBFS ceiling — ElevenLabs output level varies by 70dB+ between generations of the same voice), and sums samples per-voice-channel into one WAV.

**Caching** (`generatePathTemplate`): cache key is `md5(voiceID + ttsModel + speed + text)`; a hit is a filename-prefix glob in the cache directory — by default the subtitle file's own directory (not cwd), overridable via `parseSubtitleFile`'s `cacheDir` argument, which the session layer uses to share one cache across sessions — so the request ID isn't part of the key. If more than one file matches the prefix (e.g. a stale take from a previous request ID wasn't cleaned up), the newest by mtime wins (`newestFile`), with a warning either way. The cache-key text is truncated by **rune**, not byte, when building the filename slug — multi-byte UTF-8 (em dashes, accents) can straddle a byte cutoff and produce an invalid UTF-8 path.

**Speed resolution precedence** (used in generation and in the cache key): line's own `@speed` → speaker's configured `speed` → `default.speed` → `1.0`.

**TTS model resolution precedence**: speaker's own `tts_model` → top-level `tts_model` → built-in default `eleven_multilingual_v2`.

### Session/app layer (`session-*` commands, `serve`)

`commands/session.go` (`Session`/`SessionCue` models), `commands/service.go` (`Service` interface, `LocalService` implementation), `commands/session_store.go` (`SessionStore` interface, `FileSessionStore` — one JSON file per session), `commands/upload.go`, `commands/cleanup.go`, `commands/autofix.go`, `commands/reduce.go`, `commands/http.go`, `commands/webassets.go` + `commands/webassets/index.html`, plus their CLI wiring in `commands/session_cli.go`/`commands/autofix_cli.go`.

This is a stepped workflow — upload → cleanup → generate → reduce → export — layered on top of the engine, reachable either as CLI commands (`session-upload`, `session-cleanup`, `session-generate`, `session-reduce`, `session-export`, `session-update-cue`, `session-show`, `session-reset`, plus `session-autofix` for manual/advanced use) or as an HTTP API (`serve`, a route-for-route equivalent of Dub Studio's Flask `app.py`). `serve` embeds the frontend (`go:embed`) so it needs no files on disk beyond `--config`; `--static-dir` overrides it with a directory on disk, only useful for iterating on the frontend without a rebuild.

- **`serve` is multi-tenant, keyed by cookie.** `httpServer` (`commands/http.go`) holds `tenants map[string]*tenant`, built lazily per session ID; the ID is 16 crypto/rand bytes hex, issued in an HttpOnly cookie by `resolveTenant` on first contact (no frontend change was needed — every `fetch` in `index.html` is a relative URL, so it defaults to same-origin credentials). Each tenant's `WorkDir` is `<work-dir>/sessions/<id>/`, which scopes `session.json`, `_session.vtt` and the exported WAV per session, since all three derive from it. `validSessionID` is load-bearing, not cosmetic: the ID becomes a directory name, so a client-controlled cookie that isn't exactly 32 lowercase hex chars is discarded and replaced rather than passed to `MkdirAll`. `httpServer.mu` guards only the tenant map; session state is locked per tenant, so one browser's multi-minute `Generate` doesn't block anyone else. There is deliberately **no session expiry or eviction** — tenants accumulate in memory and on disk for the process's lifetime.
- **The mp3 cache is shared across tenants, on purpose.** `ServiceConfig.CacheDir` (pinned by `localTenantFunc` to `<work-dir>/cache/`) overrides the engine's default of "the subtitle file's own directory". The cache key is content-addressed, so a take cached by one session is byte-identical to what another would synthesize for the same line by the same voice; scoping it per session would re-synthesize a whole video for every new cookie. `parseSubtitleFile`'s `cacheDir` parameter is empty for the `run` command, i.e. the standalone engine's behavior is unchanged.
- **Every ElevenLabs failure is an error, not a `log.Fatal`.** `generateMissingVoiceLines` returns `([]AudioFile, error)`; it used to `log.Fatal`, which took the whole `serve` process down mid-request for every session using it. The TTS call is retried by `withRetry` (`commands/retry.go`): 3 attempts, 2s/4s backoff. Classification (`retryableTTSError`) is by error *type*, because that is the only signal the vendored client gives — it builds `*elevenlabs.APIError` for 400/401 and `*elevenlabs.ValidationError` for 422, and returns every other non-200 (429, 5xx, an unexpected 404) as a plain error with the response body already discarded. So typed errors are exactly the deterministic request problems and are never retried; everything else is treated as transient. Two consequences: no `Retry-After` to honor, hence blind exponential backoff, and a permanent failure arriving untyped gets retried pointlessly before surfacing. `withRetry` takes its predicate and its `sleep` as parameters so the control flow is unit-tested without a client or real delays — same pattern as `runConvergenceLoop`.
- **HTTP status codes distinguish caller fault from upstream failure.** `Generate`'s run-cap error wraps `ErrRunCapReached` and `Export`'s blocked-export error wraps `ErrExportBlocked` (sentinels, matched with `errors.Is`, because both messages are user-facing text that will get reworded). `generateErrorStatus`/`exportErrorStatus` keep 429 and 409 for those and return **502** for anything else. Before, both handlers returned one fixed status for every failure, which would have driven `index.html`'s 429-specific branch (a sticky "you've used all your runs" toast) to lie about an ElevenLabs outage. 502 falls through to the frontend's generic `!r.ok` branch, which already toasts `detail`.
- **Request logging.** `loggingMiddleware` (`commands/middleware.go`) wraps the mux and logs method, path, session, status, size and duration. Only the first 8 chars of the session id are logged — the full value is the cookie, i.e. a credential. A request with no valid session cookie logs `-`, which is normal for the very first request of a session (the cookie is issued on that *response*).
- **Still fatal, but not reachable from `serve`:** `parseSubtitleFile` keeps three `log.Fatal` calls (subtitle parse failure, bad speaker/speed spec). `upload` validates speakers up front and `UpdateCue` clamps speed into `[MinSpeed, MaxSpeed]`, so a session VTT written by `writeSessionVTT` can't reach them. Worth converting if either of those validations ever loosens.
- **`Generate` auto-converges.** Unlike the base engine's `run`, `LocalService.Generate` doesn't stop after one ElevenLabs round: it loops generate → `AutoFix` (speed-bump within a per-speaker cap, or basket if the cap isn't enough) → regenerate whatever got sped up → remeasure, silently, until `AutoFix` makes no further speed changes or `maxConvergenceRounds` (5) is hit. By the time it returns, a cue is only still flagged if speed genuinely can't fix it. This still only counts as **one** run against `RunCap` regardless of how many internal rounds ran. The round-counting/stopping control flow (`runConvergenceLoop`) and cross-round billing dedup (`aggregateGenerateRounds`) are pure and unit-tested via injected closures; the actual ElevenLabs round trip (`generateOnce`) isn't — verified live instead, same as the base engine's network-touching code.
- **`flagged` means `NeedsHuman`, not raw window overage.** `AutoFix`'s real budget (time until the same voice speaks again, `realBudgetsMs` in `commands/autofix.go`) is deliberately more generous than a cue's own subtitle window, which exists for reading text on screen, not for constraining an audio-only voice track. `cueDTO` (`commands/http.go`) reports `flagged` as `NeedsHuman`, i.e. AutoFix's own real-budget decision — a cue can run well past its subtitle window and still not be flagged, if AutoFix already decided that's fine. `Session.Flagged` (used only for the export manifest's own informational report) keeps the raw-window meaning on purpose, since that's a separate video-sync concern.
- **`Skipped`** (`SessionCue.Skipped`, toggle via `POST /cue/{index} {"skip": true/false}` or `session-update-cue --skip`) is an explicit "accept this line as-is" override: `AutoFix`, `ReduceText`, and every flagged/basket count skip over it entirely, and toggling it never touches `NeedsHuman`/`HumanReason`/text/speed, so un-skipping restores exactly what was there before.
- **`/reduce`** (`commands/reduce.go`) calls the Anthropic Messages API directly over `net/http` (forced tool use for structured output — no SDK dependency) to draft a shortened version of each still-flagged cue, sized to its real timing budget. Needs `ANTHROPIC_API_KEY` (env var, or `--anthropic-api-key`); a missing key is the one error case that surfaces up, a per-cue AI failure is just recorded as a decline. Not parity-testable against anything (it's inherently non-deterministic) — tested through the `AnthropicClient` interface (fakes for the unit suite, one real-API test gated behind the env var actually being set).

## Config file

Not committed; `config.yaml.dist` is the template. Loaded via `readConfig` with `yaml.Decoder.KnownFields(true)`, so an unrecognized key is a parse error, not silently ignored. Key fields: `auth_key`, `tts_model` (optional, global), `default` (speaker), `models` (map of named speakers), `merge_lines_threshold_ms` (optional). Each channel in the output WAV is one distinct speaker `name`, assigned via `generateModelChannelMap` (default speaker always channel 0).

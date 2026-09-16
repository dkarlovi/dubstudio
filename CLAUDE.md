# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`srt11` is a Go CLI that converts a subtitle file (`.srt` or `.vtt`) into a WAV audio track using ElevenLabs Text-to-Speech, matching the subtitle timings — for replacing a video's audio track using only its subtitles as the script.

## Commands

```sh
go build -o srt11 .              # build (first build ~16s, cached ~0.5s)
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
./srt11 run data/some_file.vtt
```
It will fail at the ElevenLabs API call with a dummy key — that's expected. A failure before that point (config parsing, subtitle parsing) is a real bug.

## Architecture

Everything lives in the `commands` package (`commands/run.go`, `commands/speaker.go`, `commands/overlap.go`, plus `_test.go` siblings); `main.go` just wires `commands.All()` into a `symfony-cli/console` app with a global `-c`/`--config` flag. There is currently one subcommand, `run` (`commands/run.go:Run`), which is a five-stage pipeline:

1. **Parse** (`parseSubtitleFile`) — reads the subtitle file via `go-astisub`, resolves each cue's speaker and optional per-line speed override, and optionally merges adjacent same-speaker cues.
2. **Resolve speaker/speed per cue** (`commands/speaker.go`) — a speaker can be set three mutually-exclusive ways: a VTT `<v Name>` tag, a `NOTE Name` comment, or a leading `[Name]` bracket tag (`resolveSpeaker`, front-anchored regex — a tag anywhere but the very start of the cue is *not* recognized as a speaker switch, and is sent to TTS as literal text). Any of these three forms can carry `@speed` (e.g. `[Matko@1.15]`, `[@1.15]` for the default speaker) via `splitSpeakerSpec`, overriding that speaker's configured speed for just that line. Speed is clamped to `[MinSpeed, MaxSpeed]` = `[0.7, 1.2]` and rejected outside that range at parse time, not clamped silently.
3. **Merge** (`canMergeCue`, inside `parseSubtitleFile`'s loop) — a pure decision function: same voice, gap under `--merge-lines-threshold-ms`/`-m`, and the merged group's total window under `--merge-max-ms` (0 = unlimited). A merged-in line's own leading speaker tag is stripped *before* folding its text in (`resolveSpeaker` only strips a tag anchored at the string's start, so this must happen per-line during the merge, not once at the end — see the comment at the strip call site if touching this).
4. **Generate** (`generateMissingVoiceLines`) — for each cue, skips synthesis if a cached mp3 already exists (see caching below); otherwise calls ElevenLabs with [request stitching](https://elevenlabs.io/docs/eleven-api/guides/how-to/text-to-speech/request-stitching) context from `previousIdsFor` (nearest preceding cues by position, regardless of speaker/speed — this used to skip speed-overridden cues and break stitching continuity, see `commands/run_test.go:TestPreviousIdsFor`) and the next few cues' IDs/text, for cross-line continuity.
5. **Check overlaps, then mix** (`findOverlaps`/`findCrossOverlaps` in `commands/overlap.go`, `generateFinalAudioFile`) — same-voice overlap is a genuine collision (each voice renders to its own output channel) and is fatal past `--overlap-tolerance-ms`/`-t` (default 0); cross-voice overlap is always reported (`(CROSS-OVERLAP Nms)`) but only fails the run if `--cross-overlap-tolerance-ms` is explicitly set (default -1 = report-only). Final mixing decodes each cue's PCM, optionally normalizes it toward `--normalize-target-db` RMS (`normalizationGain`, capped at +24dB boost and a -1dBFS ceiling — ElevenLabs output level varies by 70dB+ between generations of the same voice), and sums samples per-voice-channel into one WAV.

**Caching** (`generatePathTemplate`): cache key is `md5(voiceID + ttsModel + speed + text)`; a hit is a filename-prefix glob in the subtitle file's own directory (not cwd), so the request ID isn't part of the key. If more than one file matches the prefix (e.g. a stale take from a previous request ID wasn't cleaned up), the newest by mtime wins (`newestFile`), with a warning either way. The cache-key text is truncated by **rune**, not byte, when building the filename slug — multi-byte UTF-8 (em dashes, accents) can straddle a byte cutoff and produce an invalid UTF-8 path.

**Speed resolution precedence** (used in generation and in the cache key): line's own `@speed` → speaker's configured `speed` → `default.speed` → `1.0`.

**TTS model resolution precedence**: speaker's own `tts_model` → top-level `tts_model` → built-in default `eleven_multilingual_v2`.

## Config file

Not committed; `config.yaml.dist` is the template. Loaded via `readConfig` with `yaml.Decoder.KnownFields(true)`, so an unrecognized key is a parse error, not silently ignored. Key fields: `auth_key`, `tts_model` (optional, global), `default` (speaker), `models` (map of named speakers), `merge_lines_threshold_ms` (optional). Each channel in the output WAV is one distinct speaker `name`, assigned via `generateModelChannelMap` (default speaker always channel 0).

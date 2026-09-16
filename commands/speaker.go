package commands

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/asticode/go-astisub"
)

// Speed bounds accepted by ElevenLabs' voice settings. Values outside this
// range are rejected by the API, so we refuse them at parse time rather than
// clamping silently and producing a take nobody asked for.
const (
	MinSpeed float32 = 0.7
	MaxSpeed float32 = 1.2
)

var speakerTagRE = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*([\s\S]+)$`)

// splitSpeakerSpec splits a speaker tag into the speaker name and an optional
// per-line speed override:
//
//	"Matko"        -> name "Matko", no override
//	"Matko@1.15"   -> name "Matko", speed 1.15
//	"@1.15"        -> default speaker at speed 1.15
//
// The override wins over the speaker's configured speed for this line only.
func splitSpeakerSpec(raw string) (name string, speed float32, hasSpeed bool, err error) {
	raw = strings.TrimSpace(raw)
	at := strings.Index(raw, "@")
	if at < 0 {
		return raw, 0, false, nil
	}

	name = strings.TrimSpace(raw[:at])
	rawSpeed := strings.TrimSpace(raw[at+1:])
	if rawSpeed == "" {
		return name, 0, false, fmt.Errorf("speaker tag %q has no speed after @", raw)
	}

	parsed, convErr := strconv.ParseFloat(rawSpeed, 32)
	if convErr != nil {
		return name, 0, false, fmt.Errorf("speaker tag %q has an unparseable speed %q", raw, rawSpeed)
	}

	// Compare as float32: ParseFloat(_, 32) returns the nearest float32 widened
	// back to float64, so 0.7 arrives as 0.6999999881 and would fail a float64
	// bounds check against its own documented minimum.
	v := float32(parsed)
	if v < MinSpeed || v > MaxSpeed {
		return name, 0, false, fmt.Errorf(
			"speaker tag %q sets speed %.2f, outside the supported range %.1f-%.1f",
			raw, v, MinSpeed, MaxSpeed)
	}

	return name, v, true, nil
}

func resolveSpeaker(text string) (name, dialogue string, tagged bool) {
	m := speakerTagRE.FindStringSubmatch(text)
	if m == nil {
		return "", text, false
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), true
}

var anyTagRE = regexp.MustCompile(`\[[^\]]+\]`)

// TagViolation is a cue whose text contains a [Name]/[Name@speed] tag
// somewhere other than the very start. speakerTagRE above only recognizes a
// tag anchored at position 0 as a speaker switch; a tag anywhere else is
// sent to TTS as literal text instead. Ported from dub-studio's
// find_midcue_tag_violations (core.py), a free, upload-time guard against
// that class of bug.
type TagViolation struct {
	Index int
	Tag   string
	Text  string
}

func findMidCueTagViolations(subs *astisub.Subtitles) []TagViolation {
	violations := make([]TagViolation, 0)
	for _, item := range subs.Items {
		text := strings.TrimSpace(item.String())
		for _, loc := range anyTagRE.FindAllStringIndex(text, -1) {
			if strings.TrimSpace(text[:loc[0]]) != "" {
				violations = append(violations, TagViolation{
					Index: item.Index + 1,
					Tag:   text[loc[0]:loc[1]],
					Text:  text,
				})
				break
			}
		}
	}
	return violations
}

func lookupSpeakerModel(name string, config *Config) (SpeakerConfig, error) {
	sc, ok := config.Models[name]
	if !ok {
		return SpeakerConfig{}, fmt.Errorf(
			"speaker %q is tagged in the subtitles but is not configured under `models` (known speakers: %s)",
			name, knownSpeakerNames(config))
	}
	if sc.Model == "" {
		return SpeakerConfig{}, fmt.Errorf(
			"speaker %q is configured but has an empty voice ID (`model`)", name)
	}
	return sc, nil
}

func knownSpeakerNames(config *Config) string {
	names := make([]string, 0, len(config.Models))
	for k := range config.Models {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

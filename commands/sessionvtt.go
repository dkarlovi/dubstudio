package commands

import (
	"fmt"
	"strings"
)

// msToVTTTimestamp formats milliseconds as a WebVTT timestamp
// (HH:MM:SS.mmm). Ported from dub-studio's core._ms_to_vtt_ts.
func msToVTTTimestamp(ms int) string {
	h := ms / 3_600_000
	rem := ms % 3_600_000
	m := rem / 60_000
	rem %= 60_000
	s := rem / 1000
	rem %= 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, rem)
}

// cueTagLine is the dialogue line the engine sees, with a leading
// [Name@speed]/[Name]/[@speed] tag when needed. Ported from dub-studio's
// core._cue_tag_line, generalized from its hardcoded matko/hana pair to
// an arbitrary configured default speaker name: a speed is only tagged
// when it's an explicit override, and the voice's name is only tagged
// when it isn't the configured default -- otherwise the cue is left
// untagged so it keeps tracking that speaker's configured default speed.
func cueTagLine(c *SessionCue, defaultVoiceName string) string {
	var tag string
	switch {
	case c.SpeedOverride:
		speedStr := fmt.Sprintf("%.2f", c.Speed)
		if c.Voice == defaultVoiceName {
			tag = fmt.Sprintf("[@%s]", speedStr)
		} else {
			tag = fmt.Sprintf("[%s@%s]", c.Voice, speedStr)
		}
	case c.Voice != defaultVoiceName:
		tag = fmt.Sprintf("[%s]", c.Voice)
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", tag, c.CurrentText))
}

// writeSessionVTT serializes cues back into WebVTT with per-cue speaker/
// speed tags, ready for the engine's own parseSubtitleFile. Ported from
// dub-studio's core.write_srt11_vtt.
func writeSessionVTT(cues []*SessionCue, defaultVoiceName string) []byte {
	lines := []string{"WEBVTT", ""}
	for _, c := range cues {
		lines = append(lines,
			fmt.Sprintf("%s --> %s", msToVTTTimestamp(c.StartMs), msToVTTTimestamp(c.EndMs)),
			cueTagLine(c, defaultVoiceName),
			"",
		)
	}
	return []byte(strings.Join(lines, "\n"))
}

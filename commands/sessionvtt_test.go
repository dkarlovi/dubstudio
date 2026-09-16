package commands

import "testing"

// Mirrors dub-studio's tests/test_srt11_io.py (TestCueTagLine, TestMsToVttTs,
// TestWriteSrt11Vtt). Generalized from Python's hardcoded matko/hana pair to
// an arbitrary configured default speaker name, since the Go engine already
// supports arbitrary speaker names -- Python's prototype hardcodes two.

func TestCueTagLine(t *testing.T) {
	cases := []struct {
		name string
		cue  *SessionCue
		want string
	}{
		{
			"untagged non-default voice gets a name-only tag",
			&SessionCue{Voice: "Matko", CurrentText: "hi"},
			"[Matko] hi",
		},
		{
			"untagged default voice gets no tag",
			&SessionCue{Voice: "Hana", CurrentText: "hi"},
			"hi",
		},
		{
			"non-default voice with a speed override gets name and speed",
			&SessionCue{Voice: "Matko", CurrentText: "hi", Speed: 1.1, SpeedOverride: true},
			"[Matko@1.10] hi",
		},
		{
			"default voice with a speed override gets a bare speed tag",
			&SessionCue{Voice: "Hana", CurrentText: "hi", Speed: 1.15, SpeedOverride: true},
			"[@1.15] hi",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cueTagLine(tc.cue, "Hana"); got != tc.want {
				t.Errorf("cueTagLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMsToVTTTimestamp(t *testing.T) {
	cases := []struct {
		ms   int
		want string
	}{
		{3723004, "01:02:03.004"},
		{0, "00:00:00.000"},
	}
	for _, tc := range cases {
		if got := msToVTTTimestamp(tc.ms); got != tc.want {
			t.Errorf("msToVTTTimestamp(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestWriteSessionVTT(t *testing.T) {
	cues := []*SessionCue{
		{StartMs: 0, EndMs: 1500, Voice: "Matko", CurrentText: "hi"},
		{StartMs: 1500, EndMs: 3000, Voice: "Hana", CurrentText: "yo", Speed: 1.1, SpeedOverride: true},
	}
	want := "WEBVTT\n\n" +
		"00:00:00.000 --> 00:00:01.500\n" +
		"[Matko] hi\n\n" +
		"00:00:01.500 --> 00:00:03.000\n" +
		"[@1.10] yo\n"

	if got := string(writeSessionVTT(cues, "Hana")); got != want {
		t.Errorf("writeSessionVTT() = %q, want %q", got, want)
	}
}

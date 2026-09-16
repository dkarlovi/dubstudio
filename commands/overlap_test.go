package commands

import (
	"testing"
	"time"
)

func overlapAudioFile(voice string, offset, duration time.Duration) AudioFile {
	return AudioFile{
		Item: Item{
			Model: Model{model: voice},
		},
		Offset:   offset,
		Duration: duration,
	}
}

func TestFindOverlaps_SameVoiceAcrossADifferentVoiceCue(t *testing.T) {
	// Hana #0 / Matko #1 / Hana #2 -- Hana #0's audio spills 500ms past
	// Hana #2's start, even though Matko #1 sits in between.
	files := []AudioFile{
		overlapAudioFile("hana", 0, 3*time.Second),                     // ends at 3s
		overlapAudioFile("matko", 1*time.Second, 1*time.Second),        // ends at 2s
		overlapAudioFile("hana", 2500*time.Millisecond, 1*time.Second), // starts at 2.5s
	}

	got := findOverlaps(files, 0)
	if len(got) != 1 {
		t.Fatalf("want 1 overlap, got %+v", got)
	}
	if got[0].First != 0 || got[0].Second != 2 {
		t.Fatalf("want overlap between cue 0 and cue 2, got %+v", got[0])
	}
	if got[0].Duration != 500*time.Millisecond {
		t.Fatalf("want 500ms overlap, got %s", got[0].Duration)
	}
}

func TestFindOverlaps_SameVoiceWithinTolerance(t *testing.T) {
	// Same layout as above, but only a 100ms spill against a 120ms tolerance.
	files := []AudioFile{
		overlapAudioFile("hana", 0, 2100*time.Millisecond),      // ends at 2.1s
		overlapAudioFile("matko", 1*time.Second, 1*time.Second), // ends at 2s
		overlapAudioFile("hana", 2*time.Second, 1*time.Second),  // starts at 2s
	}

	got := findOverlaps(files, 120*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("want no overlaps within tolerance, got %+v", got)
	}
}

func TestFindOverlaps_AdjacentSameVoiceStillFlagged(t *testing.T) {
	files := []AudioFile{
		overlapAudioFile("hana", 0, 1200*time.Millisecond),     // ends at 1.2s
		overlapAudioFile("hana", 1*time.Second, 1*time.Second), // starts at 1s
	}

	got := findOverlaps(files, 0)
	if len(got) != 1 || got[0].First != 0 || got[0].Second != 1 {
		t.Fatalf("want overlap between cue 0 and cue 1, got %+v", got)
	}
	if got[0].Duration != 200*time.Millisecond {
		t.Fatalf("want 200ms overlap, got %s", got[0].Duration)
	}
}

func TestFindOverlaps_CrossVoiceNotFlagged(t *testing.T) {
	files := []AudioFile{
		overlapAudioFile("hana", 0, 1200*time.Millisecond),      // ends at 1.2s
		overlapAudioFile("matko", 1*time.Second, 1*time.Second), // starts at 1s -- different voice
	}

	got := findOverlaps(files, 0)
	if len(got) != 0 {
		t.Fatalf("want no cross-voice overlap flagged, got %+v", got)
	}
}

func TestFindCrossOverlaps_AdjacentDifferentVoiceFlagged(t *testing.T) {
	// Hana spills 400ms over the start of the very next cue, spoken by Matko.
	files := []AudioFile{
		overlapAudioFile("hana", 0, 2400*time.Millisecond),      // ends at 2.4s
		overlapAudioFile("matko", 2*time.Second, 1*time.Second), // starts at 2s
	}

	got := findCrossOverlaps(files, 0)
	if len(got) != 1 || got[0].First != 0 || got[0].Second != 1 {
		t.Fatalf("want cross-overlap between cue 0 and cue 1, got %+v", got)
	}
	if got[0].Duration != 400*time.Millisecond {
		t.Fatalf("want 400ms cross-overlap, got %s", got[0].Duration)
	}

	// The same layout must NOT be flagged as a same-voice overlap.
	if same := findOverlaps(files, 0); len(same) != 0 {
		t.Fatalf("want no same-voice overlap for a cross-voice spill, got %+v", same)
	}
}

func TestFindCrossOverlaps_WithinTolerance(t *testing.T) {
	files := []AudioFile{
		overlapAudioFile("hana", 0, 2400*time.Millisecond),
		overlapAudioFile("matko", 2*time.Second, 1*time.Second),
	}

	got := findCrossOverlaps(files, 500*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("want no cross-overlap within a 500ms tolerance, got %+v", got)
	}
}

func TestFindCrossOverlaps_PastTolerance(t *testing.T) {
	files := []AudioFile{
		overlapAudioFile("hana", 0, 2400*time.Millisecond),
		overlapAudioFile("matko", 2*time.Second, 1*time.Second),
	}

	got := findCrossOverlaps(files, 300*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("want cross-overlap flagged past a 300ms tolerance, got %+v", got)
	}
}

func TestFindCrossOverlaps_SameVoiceNotFlagged(t *testing.T) {
	files := []AudioFile{
		overlapAudioFile("hana", 0, 1200*time.Millisecond),
		overlapAudioFile("hana", 1*time.Second, 1*time.Second),
	}

	got := findCrossOverlaps(files, 0)
	if len(got) != 0 {
		t.Fatalf("want no cross-overlap for same-voice cues, got %+v", got)
	}
}

func TestFindCrossOverlaps_SkipsOverInterveningSameVoiceCue(t *testing.T) {
	// Hana / Hana / Matko -- cue 0's cross check must skip cue 1 (same voice)
	// and compare against cue 2, the first different-voice cue.
	files := []AudioFile{
		overlapAudioFile("hana", 0, 3*time.Second),                      // ends at 3s
		overlapAudioFile("hana", 1*time.Second, 1*time.Second),          // same voice, ends 2s
		overlapAudioFile("matko", 2500*time.Millisecond, 1*time.Second), // starts 2.5s -- different voice
	}

	got := findCrossOverlaps(files, 0)
	if len(got) != 1 || got[0].First != 0 || got[0].Second != 2 {
		t.Fatalf("want cross-overlap between cue 0 and cue 2, got %+v", got)
	}
}

func TestAnnotateOverlaps(t *testing.T) {
	files := []AudioFile{
		overlapAudioFile("hana", 0, 1200*time.Millisecond),
		overlapAudioFile("hana", 1*time.Second, 1*time.Second),
	}

	byFirst, flagged := annotateOverlaps(files, findOverlaps(files, 0))

	if len(flagged) != 1 || flagged[0].Overlap != 200*time.Millisecond {
		t.Fatalf("want one flagged file with a 200ms overlap, got %+v", flagged)
	}
	if ov, ok := byFirst[0]; !ok || ov.Duration != 200*time.Millisecond {
		t.Fatalf("want byFirst[0] with a 200ms overlap, got %+v ok=%v", ov, ok)
	}
}

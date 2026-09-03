package commands

import "time"

// cueOverlap records that two same-voice cues' audio will play at the same
// time: First's audio doesn't finish before Second's starts.
type cueOverlap struct {
	First, Second int
	Duration      time.Duration
}

// findOverlaps flags cues whose audio end spills past the start of the next
// cue spoken by the SAME voice -- not merely the next cue in the list. Voices
// share an output channel, so a different-voice cue sitting in between does
// not prevent the two same-voice clips from playing simultaneously.
//
// One backward pass is enough: nextByVoice tracks, per voice, the closest
// index seen so far that is greater than the current one.
func findOverlaps(files []AudioFile, tolerance time.Duration) []cueOverlap {
	overlaps := make([]cueOverlap, 0)
	nextByVoice := make(map[string]int)
	for i := len(files) - 1; i >= 0; i-- {
		voice := files[i].Item.Model.model
		if next, ok := nextByVoice[voice]; ok {
			spill := (files[i].Offset + files[i].Duration) - files[next].Offset
			if spill > tolerance {
				overlaps = append(overlaps, cueOverlap{First: i, Second: next, Duration: spill})
			}
		}
		nextByVoice[voice] = i
	}
	// walked backwards, so restore ascending order by First
	for l, r := 0, len(overlaps)-1; l < r; l, r = l+1, r-1 {
		overlaps[l], overlaps[r] = overlaps[r], overlaps[l]
	}
	return overlaps
}

// findCrossOverlaps is the mirror of findOverlaps: it flags cues whose audio
// end spills past the start of the next cue spoken by a DIFFERENT voice,
// skipping over any same-voice cues in between. Same-voice overlap is a
// physical collision -- each voice renders to its own output channel -- but
// cross-voice overlap is editorial: some crosstalk between speakers is
// natural, so this is reported separately from findOverlaps and, by the
// caller's choice, does not have to fail the run.
func findCrossOverlaps(files []AudioFile, tolerance time.Duration) []cueOverlap {
	overlaps := make([]cueOverlap, 0)
	nextByVoice := make(map[string]int)
	for i := len(files) - 1; i >= 0; i-- {
		voice := files[i].Item.Model.model

		next := -1
		for v, idx := range nextByVoice {
			if v == voice {
				continue
			}
			if next == -1 || idx < next {
				next = idx
			}
		}
		if next != -1 {
			spill := (files[i].Offset + files[i].Duration) - files[next].Offset
			if spill > tolerance {
				overlaps = append(overlaps, cueOverlap{First: i, Second: next, Duration: spill})
			}
		}

		nextByVoice[voice] = i
	}
	for l, r := 0, len(overlaps)-1; l < r; l, r = l+1, r-1 {
		overlaps[l], overlaps[r] = overlaps[r], overlaps[l]
	}
	return overlaps
}

// annotateOverlaps indexes overlaps by their First cue for per-cue lookup,
// and returns them as an ordered slice of the AudioFile that started each
// overlap with its Overlap field set to the spill duration -- the shape both
// the per-cue report and the "detected" summary block need.
func annotateOverlaps(files []AudioFile, overlaps []cueOverlap) (map[int]cueOverlap, []AudioFile) {
	byFirst := make(map[int]cueOverlap, len(overlaps))
	flagged := make([]AudioFile, 0, len(overlaps))
	for _, ov := range overlaps {
		byFirst[ov.First] = ov
		file := files[ov.First]
		file.Overlap = ov.Duration
		flagged = append(flagged, file)
	}
	return byFirst, flagged
}

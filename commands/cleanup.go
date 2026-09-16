package commands

import (
	"regexp"
	"strings"
)

// Ported from dub-studio's core.py (_clean_text/mechanical_cleanup).
// Conservative on purpose: only removes fillers when they're clearly
// parenthetical (comma-delimited) or a leading discourse marker. Anything
// that needs real judgment is NOT touched here.
var (
	cleanupLeadRE       = regexp.MustCompile(`(?i)^\s*(so|well|okay|ok|right|now|and|but|basically|honestly)\s*,\s*`)
	cleanupParenRE      = regexp.MustCompile(`(?i)\s*,\s*(you know|i mean|like|basically|honestly|actually|sort of|kind of|right|so)\s*,`)
	cleanupUmRE         = regexp.MustCompile(`(?i)\b(u+m+|u+h+|e+r+m*|hm+)\b[\s,]*`)
	cleanupSpacesRE     = regexp.MustCompile(`\s{2,}`)
	cleanupSpacePunctRE = regexp.MustCompile(`\s+([,.!?])`)
	cleanupCommaRunRE   = regexp.MustCompile(`(,\s*){2,}`)
)

func cleanText(text string) string {
	t := cleanupUmRE.ReplaceAllString(text, " ")
	t = cleanupParenRE.ReplaceAllString(t, ", ")

	m := cleanupLeadRE.ReplaceAllString(t, "")
	if m != "" && m != t {
		t = strings.ToUpper(string(m[0])) + m[1:]
	}

	t = cleanupSpacePunctRE.ReplaceAllString(t, "$1")
	t = cleanupSpacesRE.ReplaceAllString(t, " ")
	t = strings.TrimSpace(t)
	t = cleanupCommaRunRE.ReplaceAllString(t, ", ")
	return t
}

// CueEdit is one cue whose text mechanicalCleanup changed.
type CueEdit struct {
	Index  int
	Before string
	After  string
}

// mechanicalCleanup runs cleanText over each cue's text and reports which
// ones actually changed, 1-indexed to match parity.go's cue numbering.
func mechanicalCleanup(texts []string) []CueEdit {
	edited := make([]CueEdit, 0)
	for i, before := range texts {
		after := cleanText(before)
		if after != "" && after != before {
			edited = append(edited, CueEdit{Index: i + 1, Before: before, After: after})
		}
	}
	return edited
}

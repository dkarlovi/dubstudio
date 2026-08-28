package commands

import (
	"regexp"
	"strings"
	"testing"

	"github.com/asticode/go-astisub"
)

var oldRE = regexp.MustCompile(`\[(.*?)\]\s*(.+)`)

func TestResolveSpeaker(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantName     string
		wantDialogue string
		wantTagged   bool
	}{
		{"plain matko line", "[Matko] Stvarno?", "Matko", "Stvarno?", true},
		{"untagged is default speaker", "Ja mislim da je tako.", "", "Ja mislim da je tako.", false},
		{"no space after tag", "[Matko]Odmah!", "Matko", "Odmah!", true},
		{"spaces inside tag", "[ Matko ] Hej", "Matko", "Hej", true},
		{"leading whitespace before tag", "   [Matko] Hej", "Matko", "Hej", true},
		{
			"multiline dialogue keeps every line",
			"[Matko] Pa dobro,\nne razumijem baš.",
			"Matko", "Pa dobro,\nne razumijem baš.", true,
		},
		{
			"bracket used mid sentence is NOT a speaker",
			"Pa, [otprilike] tako nekako.",
			"", "Pa, [otprilike] tako nekako.", false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotDialogue, gotTagged := resolveSpeaker(tc.in)
			if gotName != tc.wantName || gotDialogue != tc.wantDialogue || gotTagged != tc.wantTagged {
				t.Errorf("resolveSpeaker(%q)\n  got  name=%q dialogue=%q tagged=%v\n  want name=%q dialogue=%q tagged=%v",
					tc.in, gotName, gotDialogue, gotTagged, tc.wantName, tc.wantDialogue, tc.wantTagged)
			}
		})
	}
}

func TestOldRegexBugs(t *testing.T) {
	multiline := "[Matko] Pa dobro,\nne razumijem baš."
	oldMatch := oldRE.FindStringSubmatch(multiline)
	if oldMatch == nil {
		t.Fatal("sanity: old regex should match this input")
	}
	if strings.Contains(oldMatch[2], "ne razumijem") {
		t.Fatal("expected the OLD regex to have dropped the second line, but it did not")
	}
	if _, newDialogue, _ := resolveSpeaker(multiline); !strings.Contains(newDialogue, "ne razumijem") {
		t.Errorf("new helper should preserve the second line, got %q", newDialogue)
	}

	aside := "Pa, [otprilike] tako nekako."
	if oldMatch = oldRE.FindStringSubmatch(aside); oldMatch == nil || oldMatch[1] != "otprilike" {
		t.Fatalf("expected OLD regex to mis-capture a mid-line bracket, got %#v", oldMatch)
	}
	if name, _, tagged := resolveSpeaker(aside); tagged || name != "" {
		t.Errorf("new helper should NOT treat a mid-line bracket as a speaker, got name=%q tagged=%v", name, tagged)
	}
}

func testConfig() *Config {
	return &Config{
		Default: SpeakerConfig{Model: "hana-voice-id", Name: "Hana", Speed: 1.2},
		Models: map[string]SpeakerConfig{
			"Matko":  {Model: "matko-voice-id", Name: "Matko", Speed: 1.0},
			"Broken": {Model: "", Name: "Broken", Speed: 1.0},
		},
	}
}

func TestLookupSpeakerModel(t *testing.T) {
	cfg := testConfig()

	t.Run("known speaker resolves", func(t *testing.T) {
		sc, err := lookupSpeakerModel("Matko", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Model != "matko-voice-id" {
			t.Errorf("got voice id %q, want %q", sc.Model, "matko-voice-id")
		}
	})

	t.Run("unknown speaker errors loudly", func(t *testing.T) {
		_, err := lookupSpeakerModel("Speaker2", cfg)
		if err == nil {
			t.Fatal("expected an error for an unconfigured speaker, got nil")
		}
		if !strings.Contains(err.Error(), "Speaker2") || !strings.Contains(err.Error(), "known speakers") {
			t.Errorf("error should name the bad speaker and list known ones, got: %v", err)
		}
	})

	t.Run("empty voice id errors loudly", func(t *testing.T) {
		if _, err := lookupSpeakerModel("Broken", cfg); err == nil {
			t.Fatal("expected an error for an empty voice id, got nil")
		} else if !strings.Contains(err.Error(), "empty voice ID") {
			t.Errorf("error should mention the empty voice ID, got: %v", err)
		}
	})
}

func TestKnownSpeakerNames(t *testing.T) {
	if got := knownSpeakerNames(testConfig()); got != "Broken, Matko" {
		t.Errorf("got %q, want %q", got, "Broken, Matko")
	}
}

func TestSplitSpeakerSpec(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantSpd  float32
		wantHas  bool
		wantErr  string
	}{
		{"Matko", "Matko", 0, false, ""},
		{"", "", 0, false, ""},
		{"Matko@1.15", "Matko", 1.15, true, ""},
		{" Matko @ 1.15 ", "Matko", 1.15, true, ""},
		{"@1.15", "", 1.15, true, ""},
		{"Matko@0.7", "Matko", 0.7, true, ""},
		{"Matko@1.2", "Matko", 1.2, true, ""},
		{"Matko@1", "Matko", 1.0, true, ""},
		{"Matko@1.4", "", 0, false, "outside the supported range"},
		{"Matko@0.5", "", 0, false, "outside the supported range"},
		{"Matko@14", "", 0, false, "outside the supported range"},
		{"Matko@", "", 0, false, "no speed after @"},
		{"Matko@fast", "", 0, false, "unparseable"},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			name, spd, has, err := splitSpeakerSpec(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q should mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.wantName || spd != tc.wantSpd || has != tc.wantHas {
				t.Errorf("splitSpeakerSpec(%q)\n  got  name=%q speed=%v hasSpeed=%v\n  want name=%q speed=%v hasSpeed=%v",
					tc.in, name, spd, has, tc.wantName, tc.wantSpd, tc.wantHas)
			}
		})
	}
}

func stitchItem(index int, id string, speed float32, override bool) Item {
	return Item{
		Sub:   &astisub.Item{Index: index},
		Path:  Path{Id: id},
		Model: Model{speed: speed, speedOverride: override},
	}
}

func TestPreviousIdsFor(t *testing.T) {
	t.Run("a normal line steps over a speed-bumped neighbour", func(t *testing.T) {
		items := []Item{
			stitchItem(0, "id0", 1.0, false),
			stitchItem(1, "id1", 1.0, false),
			stitchItem(2, "bump", 1.15, true),
			stitchItem(3, "", 1.0, false),
		}
		got := previousIdsFor(items, items[3], 3, 10)
		for _, id := range got {
			if id == "bump" {
				t.Fatalf("normal line must not stitch onto an overridden take, got %v", got)
			}
		}
		if len(got) != 2 {
			t.Fatalf("want both normal-speed ids, got %v", got)
		}
	})

	t.Run("consecutive bumps at the same speed chain onto each other", func(t *testing.T) {
		items := []Item{
			stitchItem(0, "id0", 1.0, false),
			stitchItem(1, "bumpA", 1.15, true),
			stitchItem(2, "", 1.15, true),
		}
		if got := previousIdsFor(items, items[2], 3, 10); len(got) != 1 || got[0] != "bumpA" {
			t.Fatalf("want [bumpA], got %v", got)
		}
	})

	t.Run("bumps at different speeds fall back to the normal chain", func(t *testing.T) {
		items := []Item{
			stitchItem(0, "id0", 1.0, false),
			stitchItem(1, "bumpA", 1.05, true),
			stitchItem(2, "", 1.15, true),
		}
		if got := previousIdsFor(items, items[2], 3, 10); len(got) != 1 || got[0] != "id0" {
			t.Fatalf("want the normal-speed id, got %v", got)
		}
	})

	t.Run("different speakers still stitch as before", func(t *testing.T) {
		items := []Item{
			stitchItem(0, "hana", 1.2, false),
			stitchItem(1, "", 1.0, false),
		}
		if got := previousIdsFor(items, items[1], 3, 10); len(got) != 1 || got[0] != "hana" {
			t.Fatalf("want the preceding id, got %v", got)
		}
	})

	t.Run("nothing is invented when no ids are on disk", func(t *testing.T) {
		items := []Item{stitchItem(0, "", 1.0, false), stitchItem(1, "", 1.0, false)}
		if got := previousIdsFor(items, items[1], 3, 10); len(got) != 0 {
			t.Fatalf("want no ids, got %v", got)
		}
	})

	t.Run("the first line has no history", func(t *testing.T) {
		items := []Item{stitchItem(0, "", 1.0, false)}
		if got := previousIdsFor(items, items[0], 3, 10); len(got) != 0 {
			t.Fatalf("want no ids, got %v", got)
		}
	})
}

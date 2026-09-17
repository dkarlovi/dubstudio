package commands

import (
	"testing"
)

func TestFileSessionStore_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := &FileSessionStore{Dir: dir}

	audioMs := 1800
	original := &Session{
		Name:     "video.vtt",
		RunCount: 2,
		Cues: []*SessionCue{
			{
				Index: 1, StartMs: 0, EndMs: 2000, Voice: "Hana",
				SourceText: "hi", CurrentText: "hi", Speed: 1.09, SpeedOverride: true,
				LastGeneratedText: "hi", LastGeneratedSpeed: 1.09, HasLastGenerated: true,
				AudioMs: &audioMs, CachePath: "/tmp/a.mp3", OverlapFlagged: true,
				NeedsHuman: true, HumanReason: "past cap",
			},
		},
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load() = nil, want the saved session")
	}
	if loaded.Name != original.Name || loaded.RunCount != original.RunCount {
		t.Errorf("loaded session = %+v, want name/run_count matching %+v", loaded, original)
	}
	if len(loaded.Cues) != 1 {
		t.Fatalf("want 1 cue, got %d", len(loaded.Cues))
	}
	got := loaded.Cues[0]
	if got.Voice != "Hana" || got.CurrentText != "hi" || got.Speed != 1.09 || !got.SpeedOverride {
		t.Errorf("loaded cue = %+v", got)
	}
	if got.AudioMs == nil || *got.AudioMs != 1800 {
		t.Errorf("loaded audio_ms = %v, want 1800", got.AudioMs)
	}
	if !got.NeedsHuman || got.HumanReason != "past cap" {
		t.Errorf("loaded needs_human/human_reason = %v/%q", got.NeedsHuman, got.HumanReason)
	}
	if !got.HasLastGenerated || got.LastGeneratedText != "hi" || got.LastGeneratedSpeed != 1.09 {
		t.Errorf("loaded last-generated state = %+v", got)
	}
}

func TestFileSessionStore_LoadWithNoSavedSessionReturnsNil(t *testing.T) {
	store := &FileSessionStore{Dir: t.TempDir()}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded != nil {
		t.Errorf("Load() = %+v, want nil", loaded)
	}
}

func TestFileSessionStore_Reset(t *testing.T) {
	dir := t.TempDir()
	store := &FileSessionStore{Dir: dir}
	if err := store.Save(&Session{Name: "x"}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := store.Reset(); err != nil {
		t.Fatalf("Reset() error: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded != nil {
		t.Errorf("Load() after Reset() = %+v, want nil", loaded)
	}
}

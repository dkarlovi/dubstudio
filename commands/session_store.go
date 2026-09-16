package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SessionStore persists a Session. The file-based implementation below is
// the "files first" step of the migration; a future HTTP/API service can
// depend on this same interface with a different (e.g. database-backed)
// implementation, without its callers changing.
type SessionStore interface {
	Save(s *Session) error
	Load() (*Session, error)
	Reset() error
}

// FileSessionStore persists a Session as a single JSON file in Dir,
// mirroring dub-studio's core.save/core.load (work/session.json).
type FileSessionStore struct {
	Dir string
}

func (fs *FileSessionStore) path() string {
	return filepath.Join(fs.Dir, "session.json")
}

func (fs *FileSessionStore) Save(s *Session) error {
	if err := os.MkdirAll(fs.Dir, 0o755); err != nil {
		return fmt.Errorf("creating session dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling session: %w", err)
	}
	if err := os.WriteFile(fs.path(), data, 0o644); err != nil {
		return fmt.Errorf("writing session file: %w", err)
	}
	return nil
}

// Load returns (nil, nil) if no session has been saved yet.
func (fs *FileSessionStore) Load() (*Session, error) {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading session file: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing session file: %w", err)
	}
	return &s, nil
}

func (fs *FileSessionStore) Reset() error {
	if err := os.Remove(fs.path()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing session file: %w", err)
	}
	return nil
}

package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is what survives a restart, so starting Yozora does not re-check.
type State struct {
	// CheckedAt is the last successful check; AttemptAt the last try of any kind.
	CheckedAt time.Time `json:"checked_at"`
	AttemptAt time.Time `json:"attempt_at"`
	// Latest is the newest release seen, as "0.2.N".
	Latest string `json:"latest"`
}

func loadState(path string) State {
	var s State
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if _, ok := ParseVersion(s.Latest); !ok {
		s.Latest = ""
	}
	return s
}

func saveState(path string, s State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".update-*.json")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(f.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

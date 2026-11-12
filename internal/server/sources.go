//go:build windows

package server

import (
	"errors"
	"strings"
	"sync"

	"uika-resonance/internal/config"
)

var errUnknownSource = errors.New("unknown source")

var canonicalSources = config.OrderSources

// configMu serialises every read-modify-write of the config file, whether it comes
// from the panel's endpoints or the tray menu.
var configMu sync.Mutex

// applySources saves source switches and/or the priority order through the same path
// the panel uses: load the file, change only what was asked, save, reload the holder.
// Callers validate the keys and hold configMu.
func applySources(path string, holder *config.Holder, enabled map[string]*bool, order []string) error {
	current, err := config.Load(path)
	if err != nil {
		return err
	}
	if enabled != nil {
		blocked := make([]string, 0, len(current.Sources.Blocked)+len(enabled))
		for _, block := range current.Sources.Blocked {
			if _, changed := enabled[strings.ToLower(strings.TrimSpace(block))]; !changed {
				blocked = append(blocked, block)
			}
		}
		for _, source := range config.ToggleKeys {
			if on, changed := enabled[source]; changed && !*on {
				blocked = append(blocked, source)
			}
		}
		current.Sources.Blocked = blocked
	}
	if order != nil {
		current.Sources.Order = order
	}
	if err := config.Save(current, path); err != nil {
		return err
	}
	_, err = holder.Reload()
	return err
}

// SetSourceEnabled flips one source switch from outside the panel (the tray menu).
func SetSourceEnabled(path string, holder *config.Holder, key string, on bool) error {
	known := false
	for _, source := range config.ToggleKeys {
		known = known || source == key
	}
	if !known {
		return errUnknownSource
	}
	configMu.Lock()
	defer configMu.Unlock()
	return applySources(path, holder, map[string]*bool{key: &on}, nil)
}

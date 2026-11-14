//go:build windows

package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"uika-resonance/internal/update"
)

// registerUpdate adds the update check result and the one-click install. The
// install only ever starts from a POST carrying the panel token, which the
// panel sends when the user presses Update now.
func registerUpdate(mux *http.ServeMux, d Deps, auth func(http.HandlerFunc) http.HandlerFunc) {
	write := func(w http.ResponseWriter, code int) {
		st := update.Status{Version: "", Verification: update.ModeChecksum, Install: update.InstallStatus{State: update.PhaseIdle}}
		if d.Update != nil {
			st = d.Update.Status()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(st)
	}
	mux.HandleFunc("/api/update", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", 405)
			return
		}
		write(w, http.StatusOK)
	}))
	mux.HandleFunc("/api/update/check", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if d.Update == nil {
			http.Error(w, "updates are not available in this build", http.StatusConflict)
			return
		}
		if err := d.Update.CheckNow(r.Context()); err != nil {
			code := http.StatusBadGateway
			if errors.Is(err, update.ErrBusy) || errors.Is(err, update.ErrCheckDisabled) {
				code = http.StatusConflict
			}
			http.Error(w, err.Error(), code)
			return
		}
		write(w, http.StatusOK)
	}))
	mux.HandleFunc("/api/update/install", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		if d.Update == nil {
			http.Error(w, "updates are not available in this build", http.StatusConflict)
			return
		}
		// The panel polls /api/update for progress, so this answers at once.
		if err := d.Update.StartInstall(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		write(w, http.StatusAccepted)
	}))
}

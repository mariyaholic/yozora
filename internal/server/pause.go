//go:build windows

package server

import (
	"net/http"
	"time"
)

// maxPauseMinutes caps a timed pause at a day; longer is "until I resume".
const maxPauseMinutes = 24 * 60

// pauseBody is the panel's view of the pause, the same fields /api/state carries.
func pauseBody(paused bool, until time.Time) map[string]any {
	body := map[string]any{"paused": paused}
	if paused && !until.IsZero() {
		body["paused_until"] = until
	}
	return body
}

// registerPause adds POST /api/pause. The body is {"action":"pause","minutes":60},
// {"action":"pause"} (until resumed) or {"action":"resume"}; anything else is refused.
// The pause lives in the engine's memory only.
func registerPause(mux *http.ServeMux, d Deps, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/pause", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var payload struct {
			Action  string `json:"action"`
			Minutes *int   `json:"minutes"`
		}
		if !decodeStrict(w, r, 256, &payload) {
			http.Error(w, "invalid pause payload", 400)
			return
		}
		switch payload.Action {
		case "pause":
			if payload.Minutes != nil && (*payload.Minutes < 1 || *payload.Minutes > maxPauseMinutes) {
				http.Error(w, "minutes must be between 1 and 1440", 400)
				return
			}
			var until time.Time
			if payload.Minutes != nil {
				until = time.Now().Add(time.Duration(*payload.Minutes) * time.Minute)
			}
			d.Engine.Pause(until)
		case "resume":
			if payload.Minutes != nil {
				http.Error(w, "resume takes no minutes", 400)
				return
			}
			d.Engine.Resume()
		default:
			http.Error(w, "action must be pause or resume", 400)
			return
		}
		paused, until := d.Engine.Paused(time.Now())
		writeJSON(w, 200, pauseBody(paused, until))
	}))
}

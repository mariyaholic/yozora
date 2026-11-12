//go:build windows

package cli

import (
	"log"
	"time"

	"uika-resonance/internal/player"
	"uika-resonance/internal/server"
	"uika-resonance/internal/tray"
)

var _ tray.Controller = (*daemonTray)(nil)

// MenuState is read each time the tray menu opens: the track the engine sees,
// whether sharing is paused, and the video-site switches as saved.
func (d *daemonTray) MenuState() tray.State {
	var st tray.State
	if d.eng != nil {
		s := d.eng.Status()
		st.Title, st.Artist, st.Paused = s.Title, s.Artist, s.Paused
	}
	if d.cfg != nil {
		blocked := d.cfg.Get().Sources.Blocked
		st.Sources = map[string]bool{}
		for _, key := range []string{"youtube", "twitch", "youtube_tabs"} {
			st.Sources[key] = !player.IsBlocked(player.Track{Source: key}, blocked)
		}
	}
	return st
}

func (d *daemonTray) PauseFor(dur time.Duration) {
	if d.eng != nil {
		d.eng.Pause(time.Now().Add(dur))
	}
}

func (d *daemonTray) PauseUntilResumed() {
	if d.eng != nil {
		d.eng.Pause(time.Time{})
	}
}

func (d *daemonTray) Resume() {
	if d.eng != nil {
		d.eng.Resume()
	}
}

// SetSource saves through the panel's own config path. It runs off the tray
// thread because it writes the file.
func (d *daemonTray) SetSource(key string, on bool) {
	if d.cfg == nil {
		return
	}
	go func() {
		if err := server.SetSourceEnabled(d.cfg.Path(), d.cfg, key, on); err != nil {
			log.Printf("tray: save %s switch: %v", key, err)
		}
	}()
}

//go:build windows

package presence

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
)

func rt(title string, min int) RecentTrack {
	return RecentTrack{Title: title, Artist: "A", Service: "Spotify", At: time.Unix(int64(min)*60, 0)}
}

func TestRecentNewestFirstAndCapped(t *testing.T) {
	var r recentRing
	for i := 0; i < recentMax+5; i++ {
		r.add(rt(fmt.Sprint("t", i), i))
	}
	got := r.list()
	if len(got) != recentMax {
		t.Fatalf("len %d", len(got))
	}
	if got[0].Title != "t24" || got[recentMax-1].Title != "t5" {
		t.Fatalf("order %s ... %s", got[0].Title, got[recentMax-1].Title)
	}
}

func TestRecentDedupes(t *testing.T) {
	var r recentRing
	r.add(rt("a", 1))
	r.add(rt("b", 2))
	// Same track again at the front (a resume, or art arriving late): no new entry, art refreshed.
	b := rt("b", 3)
	b.ArtURL = "http://art/b.jpg"
	r.add(b)
	got := r.list()
	if len(got) != 2 || got[0].ArtURL != "http://art/b.jpg" || !got[0].At.Equal(time.Unix(120, 0)) {
		t.Fatalf("front repeat: %+v", got)
	}
	// A track heard earlier moves up with its new time and keeps its art.
	r.add(rt("a", 5))
	got = r.list()
	if len(got) != 2 || got[0].Title != "a" || !got[0].At.Equal(time.Unix(300, 0)) {
		t.Fatalf("replay: %+v", got)
	}
	// Different service or artist is a different track.
	other := rt("a", 6)
	other.Service = "YouTube Music"
	r.add(other)
	if len(r.list()) != 3 {
		t.Fatalf("service ignored: %+v", r.list())
	}
	r.add(RecentTrack{})
	if len(r.list()) != 3 {
		t.Fatal("untitled entry recorded")
	}
}

func TestRecentListIsACopy(t *testing.T) {
	var r recentRing
	r.add(rt("a", 1))
	l := r.list()
	l[0].Title = "changed"
	if r.list()[0].Title != "a" {
		t.Fatal("list leaked internal storage")
	}
}

func TestEnginePublishRecordsRecent(t *testing.T) {
	holder, err := config.NewHolder(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	track := player.Track{Source: "spotify", Player: "Spotify", Title: "song", Artist: "artist", Playing: true, LastUpdated: now, DurationSec: 200}
	e := &Engine{Cfg: holder, ipc: &recordingActivityClient{}, lastSMTCSessions: []player.Track{track}}
	e.step(now)
	e.step(now.Add(2 * time.Second))
	got := e.Recent()
	if len(got) != 1 || got[0].Title != "song" || got[0].Artist != "artist" || got[0].Service == "" {
		t.Fatalf("recent after publish: %+v", got)
	}
}

//go:build windows

package presence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"uika-resonance/internal/art"
	"uika-resonance/internal/config"
	"uika-resonance/internal/discordipc"
	"uika-resonance/internal/player"
)

func TestBuildActivityKeepsTrackPayloadLean(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	track := &player.Track{
		Title:       "Let Down",
		Artist:      "Radiohead",
		Playing:     true,
		PositionSec: 81.49,
		DurationSec: 275.756,
		LastUpdated: now,
	}

	activity := buildActivity("Yozora", discordipc.TypeListening, 0, "Let Down", "Radiohead", track, now)
	if activity.Name != "Yozora" || activity.Type != discordipc.TypeListening {
		t.Fatalf("activity identity = (%q, %d), want (Yozora, %d)", activity.Name, activity.Type, discordipc.TypeListening)
	}
	if activity.Details != "Let Down" || activity.State != "Radiohead" {
		t.Fatalf("activity text = (%q, %q), want (Let Down, Radiohead)", activity.Details, activity.State)
	}
	if activity.Assets != nil || len(activity.Buttons) != 0 || activity.Instance {
		t.Fatalf("base activity unexpectedly contains decorations: assets=%+v buttons=%+v instance=%t", activity.Assets, activity.Buttons, activity.Instance)
	}
	if activity.Timestamps == nil {
		t.Fatal("playing track has no timestamps")
	}
	wantStart := now.Unix() - int64(track.ElapsedSec(now))
	wantEnd := wantStart + int64(track.DurationSec)
	if activity.Timestamps.Start != wantStart || activity.Timestamps.End != wantEnd {
		t.Fatalf("timestamps = %+v, want start=%d end=%d", activity.Timestamps, wantStart, wantEnd)
	}
}

type recordingActivityClient struct {
	activities []*discordipc.Activity
	err        error
}

func (c *recordingActivityClient) Alive() bool                      { return true }
func (c *recordingActivityClient) Close()                           {}
func (c *recordingActivityClient) Ping() error                      { return nil }
func (c *recordingActivityClient) AcceptedPayload() json.RawMessage { return nil }
func (c *recordingActivityClient) SetActivity(a *discordipc.Activity) error {
	c.activities = append(c.activities, a)
	return c.err
}

type waitingArtwork struct {
	release chan struct{}
	lookup  art.Lookup
}

func (a *waitingArtwork) Resolve(art.Track, string) art.Lookup {
	<-a.release
	return a.lookup
}
func (a *waitingArtwork) DefaultURL() string { return "" }

func TestSlowArtworkDoesNotDelayMetadataAndIsPublishedWhenReady(t *testing.T) {
	holder, err := config.NewHolder(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolver := &waitingArtwork{
		release: make(chan struct{}),
		lookup:  art.Lookup{ImageURL: "https://example.test/cover.jpg", ListenURL: "https://example.test/track", Via: "itunes"},
	}
	defer func() {
		select {
		case <-resolver.release:
		default:
			close(resolver.release)
		}
	}()
	client := &recordingActivityClient{}
	now := time.Now()
	track := player.Track{Source: "spotify", Player: "Spotify", Title: "current track", Artist: "current artist", Album: "current album", Playing: true, LastUpdated: now, DurationSec: 200}
	engine := &Engine{Cfg: holder, Art: resolver, ipc: client, lastSMTCSessions: []player.Track{track}}
	engine.step(now)
	start := time.Now()
	engine.step(now.Add(2 * time.Second))
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("metadata waited %s for unresolved artwork", elapsed)
	}
	if len(client.activities) != 1 || client.activities[0].Details != track.Title {
		t.Fatalf("metadata not published while artwork is blocked: %+v", client.activities)
	}

	close(resolver.release)
	deadline := time.Now().Add(time.Second)
	for engine.Status().ArtURL != resolver.lookup.ImageURL && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if engine.Status().ArtURL != resolver.lookup.ImageURL {
		t.Fatal("background artwork never became ready")
	}
	engine.step(now.Add(20 * time.Second))
	if len(client.activities) != 2 {
		t.Fatalf("published %d activities, want metadata followed by artwork enrichment", len(client.activities))
	}
	activity := client.activities[1]
	if activity.Assets == nil || activity.Assets.LargeImage != resolver.lookup.ImageURL || activity.Assets.LargeText != track.Album || activity.Assets.SmallText != holder.Get().Discord.AppName {
		t.Fatalf("artwork/template fields not preserved: %+v", activity.Assets)
	}
	if len(activity.Buttons) != 2 || activity.Buttons[0].URL != musicURL(&track, resolver.lookup.ListenURL, true) || activity.Buttons[1].Label != "Yozora" {
		t.Fatalf("listen button not preserved: %+v", activity.Buttons)
	}
}

type artworkRequest struct {
	track  art.Track
	result chan art.Lookup
}

type queuedArtwork struct {
	requests chan artworkRequest
	release  chan struct{}
}

func (a *queuedArtwork) Resolve(track art.Track, _ string) art.Lookup {
	request := artworkRequest{track: track, result: make(chan art.Lookup, 1)}
	a.requests <- request
	select {
	case lookup := <-request.result:
		return lookup
	case <-a.release:
		return art.Lookup{}
	}
}
func (a *queuedArtwork) DefaultURL() string { return "" }

func newArtworkTestEngine(t *testing.T, track player.Track) (*Engine, *queuedArtwork) {
	t.Helper()
	holder, err := config.NewHolder(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	resolver := &queuedArtwork{requests: make(chan artworkRequest, 4), release: make(chan struct{})}
	t.Cleanup(func() { close(resolver.release) })
	return &Engine{Cfg: holder, Art: resolver, ipc: &recordingActivityClient{}, lastSMTCSessions: []player.Track{track}}, resolver
}

func nextArtworkRequest(t *testing.T, resolver *queuedArtwork, title string) artworkRequest {
	t.Helper()
	synctest.Wait()
	select {
	case request := <-resolver.requests:
		if request.track.Title != title {
			t.Fatalf("artwork requested for %q, want %q", request.track.Title, title)
		}
		return request
	default:
		t.Fatalf("no artwork request for %q", title)
		return artworkRequest{}
	}
}

func TestArtworkRestartsAfterNilGap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		track := player.Track{Source: "spotify", Title: "returning track", Artist: "artist", Playing: true, LastUpdated: now}
		engine, resolver := newArtworkTestEngine(t, track)
		engine.step(now)
		first := nextArtworkRequest(t, resolver, track.Title)

		engine.lastSMTCSessions = nil
		engine.step(now.Add(250 * time.Millisecond))
		first.result <- art.Lookup{ImageURL: "https://example.test/obsolete.jpg"}
		synctest.Wait()
		if got := engine.Status().ArtURL; got != "" {
			t.Fatalf("artwork applied with no selected track: %q", got)
		}

		engine.lastSMTCSessions = []player.Track{track}
		engine.step(now.Add(500 * time.Millisecond))
		second := nextArtworkRequest(t, resolver, track.Title)
		second.result <- art.Lookup{ImageURL: "https://example.test/current.jpg"}
		synctest.Wait()
		if got := engine.Status().ArtURL; got != "https://example.test/current.jpg" {
			t.Fatalf("returning track artwork = %q, want current lookup", got)
		}
		engine.step(now.Add(2 * time.Second))
		client := engine.ipc.(*recordingActivityClient)
		if len(client.activities) != 1 || client.activities[0].Assets.LargeImage != engine.Status().ArtURL {
			t.Fatalf("returning track artwork not published: %+v", client.activities)
		}
	})
}

func TestArtworkIgnoresOutOfOrderSelections(t *testing.T) {
	for _, gap := range []string{"different track", "nil gap"} {
		t.Run(gap, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "spotify", Title: "returning track", Artist: "artist", Playing: true, LastUpdated: now}
				engine, resolver := newArtworkTestEngine(t, track)
				engine.step(now)
				first := nextArtworkRequest(t, resolver, track.Title)

				var other artworkRequest
				engine.lastSMTCSessions = nil
				if gap == "different track" {
					otherTrack := track
					otherTrack.Title = "other track"
					engine.lastSMTCSessions = []player.Track{otherTrack}
				}
				engine.step(now.Add(250 * time.Millisecond))
				if gap == "different track" {
					other = nextArtworkRequest(t, resolver, "other track")
				}

				engine.lastSMTCSessions = []player.Track{track}
				engine.step(now.Add(500 * time.Millisecond))
				current := nextArtworkRequest(t, resolver, track.Title)
				want := art.Lookup{ImageURL: "https://example.test/current.jpg", ListenURL: "https://example.test/current", Via: "current"}
				current.result <- want
				synctest.Wait()
				if gap == "different track" {
					other.result <- art.Lookup{ImageURL: "https://example.test/other.jpg", ListenURL: "https://example.test/other", Via: "other"}
					synctest.Wait()
					if got := engine.Status().ArtURL; got != want.ImageURL {
						t.Fatalf("other track's late lookup replaced current artwork: %q", got)
					}
				}
				first.result <- art.Lookup{ImageURL: "https://example.test/obsolete.jpg", ListenURL: "https://example.test/obsolete", Via: "obsolete"}
				synctest.Wait()
				if got := engine.Status(); got.ArtURL != want.ImageURL || got.Via != want.Via || engine.lastListenURL != want.ListenURL {
					t.Fatalf("old selection replaced returning identity's artwork: art=%q listen=%q via=%q", got.ArtURL, engine.lastListenURL, got.Via)
				}
			})
		})
	}
}

func TestLoopbackArtworkStaysDashboardOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		track := player.Track{Source: "browser", Title: "fixture video", Artist: "fixture channel", Playing: true, LastUpdated: now}
		engine, resolver := newArtworkTestEngine(t, track)
		engine.Cfg.Get().Sources.Blocked = nil
		client := engine.ipc.(*recordingActivityClient)
		engine.step(now)
		request := nextArtworkRequest(t, resolver, track.Title)
		local := art.Lookup{ImageURL: "http://127.0.0.1:49152/art/thumb.jpg", Via: "smtc"}
		request.result <- local
		synctest.Wait()

		engine.step(now.Add(2 * time.Second))
		if len(client.activities) != 1 || client.activities[0] == nil {
			t.Fatalf("metadata not published exactly once: %d frames", len(client.activities))
		}
		if got := client.activities[0].Assets.LargeImage; got != "" {
			t.Fatalf("loopback artwork sent to Discord: %q", got)
		}
		if got := engine.Status().ArtURL; got != local.ImageURL {
			t.Fatalf("dashboard artwork lost: %q", got)
		}

		engine.step(now.Add(20 * time.Second))
		engine.step(now.Add(40 * time.Second))
		if len(client.activities) != 1 {
			t.Fatalf("local artwork churned Discord sends: %d frames", len(client.activities))
		}
	})
}

func TestTrackTransitionPersistsInvalidatedArtwork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		old := player.Track{Source: "spotify", Title: "old track", Artist: "artist", Playing: true, LastUpdated: now}
		current := old
		current.Title = "new track"
		engine, resolver := newArtworkTestEngine(t, current)
		engine.StateDir = t.TempDir()
		engine.cur = &old
		engine.lastIdentity = old.IdentityPlaying()
		engine.lastArtURL = "https://example.test/old.jpg"
		engine.lastListenURL = "https://example.test/old"
		engine.lastVia = "old"

		engine.step(now)
		request := nextArtworkRequest(t, resolver, current.Title)
		status := engine.Status()
		if status.Title != current.Title || status.ArtURL != "" || status.Via != "" || engine.lastListenURL != "" {
			t.Fatalf("new selection retained old artwork metadata: status=%+v listen=%q", status, engine.lastListenURL)
		}
		data, err := os.ReadFile(filepath.Join(engine.StateDir, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		var persisted Status
		if err := json.Unmarshal(data, &persisted); err != nil {
			t.Fatal(err)
		}
		if persisted.Title != current.Title || persisted.ArtURL != "" || persisted.Via != "" {
			t.Fatalf("persisted new title with old artwork: title=%q art=%q via=%q", persisted.Title, persisted.ArtURL, persisted.Via)
		}
		request.result <- art.Lookup{ImageURL: "https://example.test/current.jpg"}
		synctest.Wait()
	})
}

func TestHiddenPauseClearIsIdempotent(t *testing.T) {
	for _, mode := range []string{"hide", "clear"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "spotify", Title: "track", Artist: "artist", Playing: true, LastUpdated: now}
				engine, resolver := newArtworkTestEngine(t, track)
				engine.Cfg.Get().Presence.PausedMode = mode
				client := engine.ipc.(*recordingActivityClient)
				engine.step(now)
				playing := nextArtworkRequest(t, resolver, track.Title)
				playing.result <- art.Lookup{ImageURL: "https://example.test/playing.jpg"}
				synctest.Wait()
				engine.step(now.Add(time.Second))
				if len(client.activities) != 1 || client.activities[0] == nil {
					t.Fatal("initial playback was not published")
				}

				track.Playing = false
				engine.lastSMTCSessions = []player.Track{track}
				engine.step(now.Add(2 * time.Second))
				paused := nextArtworkRequest(t, resolver, track.Title)
				engine.step(now.Add(3 * time.Second))
				if len(client.activities) != 1 {
					t.Fatal("pause cleared before publication rate gate")
				}
				clearedAt := now.Add(6 * time.Second)
				engine.step(clearedAt)
				if len(client.activities) != 2 || client.activities[1] != nil || !engine.cleared || engine.Status().Published != nil {
					t.Fatalf("pause did not clear activity: %+v", client.activities)
				}
				if !engine.Status().LastSend.Equal(clearedAt) || engine.sentID != track.IdentityPlaying() {
					t.Errorf("clear bookkeeping: lastSend=%s sentID=%q, want %s and paused identity", engine.Status().LastSend, engine.sentID, clearedAt)
				}
				paused.result <- art.Lookup{ImageURL: "https://example.test/paused.jpg"}
				synctest.Wait()
				for offset := 250 * time.Millisecond; offset <= 6*time.Second; offset += 250 * time.Millisecond {
					engine.step(clearedAt.Add(offset))
				}
				if len(client.activities) != 2 {
					t.Fatalf("hidden pause published %d frames, want one activity and one clear even after artwork arrives", len(client.activities))
				}
				if !engine.Status().LastSend.Equal(clearedAt) {
					t.Fatal("idempotent clear advanced lastSend without publication")
				}
			})
		})
	}
}

func TestHiddenPauseResumeHonorsClearRateGap(t *testing.T) {
	for _, mode := range []string{"hide", "clear"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			track := player.Track{Source: "spotify", Title: "track", Artist: "artist", Playing: true, LastUpdated: now}
			engine, _ := newArtworkTestEngine(t, track)
			engine.Art = nil
			engine.Cfg.Get().Presence.PausedMode = mode
			client := engine.ipc.(*recordingActivityClient)
			engine.step(now)
			engine.step(now.Add(time.Second))
			track.Playing = false
			engine.lastSMTCSessions = []player.Track{track}
			engine.step(now.Add(2 * time.Second))
			clearedAt := now.Add(6 * time.Second)
			engine.step(clearedAt)
			if len(client.activities) != 2 || client.activities[1] != nil {
				t.Fatal("pause was not cleared")
			}

			track.Playing = true
			engine.lastSMTCSessions = []player.Track{track}
			engine.step(clearedAt.Add(250 * time.Millisecond))
			engine.step(clearedAt.Add(time.Second))
			engine.step(clearedAt.Add(5*time.Second - time.Millisecond))
			if len(client.activities) != 2 {
				t.Fatalf("resume published before clear's rate gate: %d frames", len(client.activities))
			}
			engine.step(clearedAt.Add(5 * time.Second))
			if len(client.activities) != 3 || client.activities[2] == nil || engine.cleared || engine.sentID != track.IdentityPlaying() {
				t.Fatalf("resume did not republish after clear's rate gate: frames=%d cleared=%t sentID=%q", len(client.activities), engine.cleared, engine.sentID)
			}
		})
	}
}

func TestPausedVisibilityPolicyChangeHonorsRateGap(t *testing.T) {
	for _, modes := range []struct{ from, to string }{
		{"hide", "show"},
		{"clear", "show"},
		{"show", "hide"},
		{"show", "clear"},
	} {
		t.Run(modes.from+"->"+modes.to, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				track := player.Track{Source: "spotify", Title: "paused track", Artist: "artist", Playing: false, LastUpdated: now}
				engine, resolver := newArtworkTestEngine(t, track)
				cfg := engine.Cfg.Get()
				cfg.Presence.PausedMode = modes.from
				gap := time.Duration(cfg.Presence.MinUpdateSecs) * time.Second
				client := engine.ipc.(*recordingActivityClient)
				engine.step(now)
				request := nextArtworkRequest(t, resolver, track.Title)
				cover := "https://example.test/settled.jpg"
				request.result <- art.Lookup{ImageURL: cover}
				synctest.Wait()
				initialAt := now.Add(time.Second)
				engine.step(initialAt)
				initiallyCleared := modes.from != "show"
				if len(client.activities) != 1 || (client.activities[0] == nil) != initiallyCleared || engine.cleared != initiallyCleared {
					t.Fatalf("initial %s policy not applied: frames=%d cleared=%t", modes.from, len(client.activities), engine.cleared)
				}
				if engine.sentID != track.IdentityPlaying() || engine.lastArtURL != cover || engine.sentArtURL != cover {
					t.Fatal("initial publication did not settle the paused identity and artwork")
				}

				cfg.Presence.PausedMode = modes.to
				for _, offset := range []time.Duration{250 * time.Millisecond, time.Second, gap - time.Millisecond} {
					engine.step(initialAt.Add(offset))
				}
				if len(client.activities) != 1 || !engine.Status().LastSend.Equal(initialAt) || engine.cleared != initiallyCleared {
					t.Fatalf("%s policy published before rate gate: frames=%d cleared=%t", modes.to, len(client.activities), engine.cleared)
				}
				changedAt := initialAt.Add(gap)
				engine.step(changedAt)
				wantCleared := modes.to != "show"
				if len(client.activities) != 2 || (client.activities[1] == nil) != wantCleared || engine.cleared != wantCleared {
					t.Fatalf("%s policy not published after rate gate: frames=%d cleared=%t", modes.to, len(client.activities), engine.cleared)
				}
				status := engine.Status()
				if !status.LastSend.Equal(changedAt) || status.Published != client.activities[1] || engine.sentID != track.IdentityPlaying() || engine.sentArtURL != cover {
					t.Fatal("policy publication did not record the unchanged paused identity and artwork")
				}
				if !wantCleared && (status.Title != track.Title || status.Playing || status.Published.Assets == nil || status.Published.Assets.LargeImage != cover || status.Published.Timestamps != nil) {
					t.Fatalf("shown paused activity lost settled metadata: %+v", status.Published)
				}
				engine.step(changedAt.Add(gap))
				engine.step(changedAt.Add(2 * gap))
				if len(client.activities) != 2 || !engine.Status().LastSend.Equal(changedAt) {
					t.Fatal("steady visibility policy was not idempotent")
				}

				cfg.Presence.PausedMode = modes.from
				client.err = errors.New("synthetic visibility rejection")
				failedAt := changedAt.Add(3 * gap)
				engine.step(failedAt)
				if len(client.activities) != 3 || (client.activities[2] == nil) != initiallyCleared || engine.cleared != wantCleared || !engine.Status().LastSend.Equal(changedAt) || engine.Status().Published != status.Published {
					t.Fatal("failed policy publication changed successful visibility bookkeeping")
				}
				wantErr := "send: synthetic visibility rejection"
				if initiallyCleared {
					wantErr = "clear: synthetic visibility rejection"
				}
				if engine.Status().LastErr != wantErr || engine.sentID != track.IdentityPlaying() || engine.sentArtURL != cover {
					t.Fatal("failed policy publication did not preserve error and settled metadata")
				}
				client.err = nil
				engine.ipc = client
				retriedAt := failedAt.Add(250 * time.Millisecond)
				engine.step(retriedAt)
				if len(client.activities) != 4 || (client.activities[3] == nil) != initiallyCleared || engine.cleared != initiallyCleared || !engine.Status().LastSend.Equal(retriedAt) || engine.Status().Published != client.activities[3] || engine.Status().LastErr != "" {
					t.Fatal("failed policy publication was not retried successfully")
				}
				engine.step(retriedAt.Add(gap))
				if len(client.activities) != 4 || !engine.Status().LastSend.Equal(retriedAt) {
					t.Fatal("successful policy retry was not idempotent")
				}
				synctest.Wait()
				select {
				case <-resolver.requests:
					t.Fatal("visibility policy change restarted settled artwork")
				default:
				}
			})
		})
	}
}

func TestFailedHiddenPauseClearRemainsRetriable(t *testing.T) {
	now := time.Now()
	track := player.Track{Source: "spotify", Title: "track", Artist: "artist", Playing: true, LastUpdated: now}
	engine, _ := newArtworkTestEngine(t, track)
	engine.Art = nil
	engine.Cfg.Get().Presence.PausedMode = "hide"
	client := engine.ipc.(*recordingActivityClient)
	engine.step(now)
	engine.step(now.Add(time.Second))
	track.Playing = false
	engine.lastSMTCSessions = []player.Track{track}
	engine.step(now.Add(2 * time.Second))
	client.err = errors.New("synthetic clear rejection")
	engine.step(now.Add(6 * time.Second))
	if len(client.activities) != 2 || engine.cleared || !engine.lastSend.Equal(now.Add(time.Second)) || engine.sentID == track.IdentityPlaying() || engine.Status().Published == nil {
		t.Fatalf("failed clear changed successful-publication bookkeeping: frames=%d status=%+v", len(client.activities), engine.Status())
	}
	if engine.Status().LastErr != "clear: synthetic clear rejection" {
		t.Fatalf("failed clear error = %q", engine.Status().LastErr)
	}

	client.err = nil
	engine.ipc = client
	retriedAt := now.Add(6250 * time.Millisecond)
	engine.step(retriedAt)
	if len(client.activities) != 3 || client.activities[2] != nil || !engine.cleared || !engine.lastSend.Equal(retriedAt) || engine.sentID != track.IdentityPlaying() || engine.Status().LastErr != "" {
		t.Fatalf("failed clear was not retried and recorded on success: frames=%d status=%+v", len(client.activities), engine.Status())
	}
	engine.step(retriedAt.Add(250 * time.Millisecond))
	if len(client.activities) != 3 {
		t.Fatal("successful retry was cleared again")
	}
}

func TestRevokedPublishedSourceClearsAtRateGateAndCanOptBackIn(t *testing.T) {
	for _, filter := range []string{"BROWSER", "firefox"} {
		t.Run(filter, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.toml")
			cfg := config.Defaults()
			cfg.Sources.Blocked = []string{}
			if err := config.Save(cfg, path); err != nil {
				t.Fatal(err)
			}
			holder, err := config.NewHolder(path)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			tr := player.Track{Source: "browser", AppID: "org.mozilla.firefox", Title: "synthetic video", Playing: true, LastUpdated: now}
			client := &recordingActivityClient{}
			e := &Engine{Cfg: holder, ipc: client, lastSMTCSessions: []player.Track{tr}}
			e.step(now)
			e.step(now.Add(time.Second))
			if len(client.activities) != 1 {
				t.Fatal("fixture not published")
			}

			e.lastSMTCSessions = nil
			e.step(now.Add(2 * time.Second))
			cfg.Sources.Blocked = []string{filter}
			if err := config.Save(cfg, path); err != nil {
				t.Fatal(err)
			}
			if _, err := holder.Reload(); err != nil {
				t.Fatal(err)
			}
			e.step(now.Add(5999 * time.Millisecond))
			if len(client.activities) != 1 {
				t.Fatal("cleared before rate gate")
			}
			clearedAt := now.Add(6 * time.Second)
			e.step(clearedAt)
			if len(client.activities) != 2 || client.activities[1] != nil || e.Status().Published != nil {
				t.Fatalf("revoked source retained until idle timeout: frames=%d", len(client.activities))
			}
			for i := 1; i <= 20; i++ {
				e.step(clearedAt.Add(time.Duration(i) * time.Second))
			}
			if len(client.activities) != 2 || !e.Status().LastSend.Equal(clearedAt) {
				t.Fatal("repeated clear not idempotent")
			}
			cfg.Sources.Blocked = []string{}
			if err := config.Save(cfg, path); err != nil {
				t.Fatal(err)
			}
			if _, err := holder.Reload(); err != nil {
				t.Fatal(err)
			}
			e.lastSMTCSessions = []player.Track{tr}
			e.step(now.Add(30 * time.Second))
			e.step(now.Add(31 * time.Second))
			if len(client.activities) != 3 || client.activities[2] == nil {
				t.Fatal("opt-back-in did not publish")
			}
		})
	}
}

func TestDisabledBrowserFallsBackToMusicWithoutClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.toml")
	cfg := config.Defaults()
	cfg.Sources.Blocked = []string{}
	cfg.Sources.Order = []string{"browser", "spotify"}
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	holder, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	browser := player.Track{Source: "browser", AppID: "org.mozilla.firefox", Title: "synthetic video", Playing: true, LastUpdated: now}
	music := player.Track{Source: "spotify", Title: "synthetic music", Playing: true, LastUpdated: now}
	client := &recordingActivityClient{}
	e := &Engine{Cfg: holder, ipc: client, lastSMTCSessions: []player.Track{browser, music}}
	e.step(now)
	e.step(now.Add(time.Second))
	if len(client.activities) != 1 || client.activities[0].Details != browser.Title {
		t.Fatal("fixture did not publish the browser session first")
	}
	cfg.Sources.Blocked = []string{"browser"}
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Reload(); err != nil {
		t.Fatal(err)
	}
	e.step(now.Add(2 * time.Second))
	e.step(now.Add(6 * time.Second))
	if len(client.activities) != 2 || client.activities[1] == nil || client.activities[1].Details != music.Title {
		t.Fatalf("disabling browser did not fall back to music: %+v", client.activities)
	}
}

func TestSetErrPersistsFailureForStatusCommand(t *testing.T) {
	stateDir := t.TempDir()
	engine := &Engine{StateDir: stateDir}
	engine.setErr("send: discordipc: SET_ACTIVITY failed: code=4006 authentication required")

	data, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatalf("read persisted state: %v", err)
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("decode persisted state: %v", err)
	}
	if status.LastErr == "" {
		t.Fatal("persisted status has no last_error")
	}
}

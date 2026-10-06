//go:build windows

// Package presence is the heart of Yozora: it arbitrates media
// sources, renders templates, enforces Discord's update cadence and pushes
// SET_ACTIVITY frames.
package presence

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"uika-resonance/internal/art"
	"uika-resonance/internal/config"
	"uika-resonance/internal/discordipc"
	"uika-resonance/internal/player"
	"uika-resonance/internal/smtc"
	"uika-resonance/internal/template"
)

// Status is a snapshot for the dashboard and status command.
type Status struct {
	UpdatedAt    time.Time            `json:"updated_at"`
	Connected    bool                 `json:"discord_connected"`
	Playing      bool                 `json:"playing"`
	Source       string               `json:"source,omitempty"`
	Service      string               `json:"service,omitempty"`
	SmallImage   string               `json:"small_image,omitempty"`
	Published    *discordipc.Activity `json:"published_activity,omitempty"`
	Acknowledged json.RawMessage      `json:"discord_activity_readback,omitempty"`
	Title        string               `json:"title,omitempty"`
	Artist       string               `json:"artist,omitempty"`
	Album        string               `json:"album,omitempty"`
	Elapsed      float64              `json:"elapsed_sec"`
	Duration     float64              `json:"duration_sec"`
	LastSend     time.Time            `json:"last_send,omitempty"`
	ArtURL       string               `json:"art_url,omitempty"`
	Via          string               `json:"art_via,omitempty"`
	LastErr      string               `json:"last_error,omitempty"`
	Sends        int64                `json:"sends"`
}

type artworkResolver interface {
	Resolve(art.Track, string) art.Lookup
	DefaultURL() string
}

type activityClient interface {
	Alive() bool
	SetActivity(*discordipc.Activity) error
	AcceptedPayload() json.RawMessage
	Ping() error
	Close()
}

type Engine struct {
	Cfg      *config.Holder
	Art      artworkResolver
	StateDir string

	smtcCh    <-chan []smtc.Track
	spotifyCh <-chan player.Track

	mu               sync.Mutex
	stateMu          sync.Mutex
	ipc              activityClient
	cur              *player.Track
	spotify          *player.Track
	lastSMTCSessions []player.Track
	identityAt       time.Time
	lastIdentity     string
	lastSend         time.Time
	sentID           string
	sentElapsedAt    time.Time
	sentElapsedSec   float64
	lastActivity     time.Time
	cleared          bool
	nextDial         atomic.Int64
	sends            int64
	lastErr          string
	lastArtURL       string
	lastListenURL    string
	lastVia          string
	artGeneration    uint64
	sentArtURL       string
	published        *discordipc.Activity
	publishedTrack   *player.Track // source/AppID of the last successful visible publication
	acknowledged     json.RawMessage
}

func New(cfg *config.Holder, a *art.Resolver, smtcCh <-chan []smtc.Track, spotifyCh <-chan player.Track, stateDir string) *Engine {
	return &Engine{
		Cfg: cfg, Art: a,
		smtcCh: smtcCh, spotifyCh: spotifyCh,
		StateDir: stateDir,
	}
}

// Run drives the engine until ctx is done.
func (e *Engine) Run(ctx context.Context) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	// First snapshot so `status` works before any Discord send.
	e.writeState()
	for {
		select {
		case <-ctx.Done():
			e.disconnect()
			return
		case tracks := <-e.smtcCh:
			e.ingest(tracks)
		case tr := <-e.spotifyCh:
			sp := tr
			e.mu.Lock()
			e.spotify = &sp
			e.mu.Unlock()
		case <-tick.C:
			e.step(time.Now())
		}
	}
}

// ingest adapts an SMTC snapshot into the unified model.
func (e *Engine) ingest(tracks []smtc.Track) {
	out := make([]player.Track, 0, len(tracks)+1)
	for _, t := range tracks {
		out = append(out, player.Track{
			Source:      smtc.Classify(t.AppID),
			AppID:       t.AppID,
			Player:      smtc.FriendlyName(t.AppID),
			Title:       t.Title,
			Artist:      firstNonEmpty(t.Artist, t.AlbumArtist, t.Subtitle),
			Album:       t.AlbumTitle,
			TrackNumber: t.TrackNumber,
			Playing:     t.Playing,
			PositionSec: t.PositionSec,
			DurationSec: t.DurationSec,
			LastUpdated: t.LastUpdated,
			Thumb:       t.Thumb,
		})
	}
	e.mu.Lock()
	e.lastSMTCSessions = out
	e.mu.Unlock()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// pick arbitrates across SMTC sessions and the (optional) fresh Spotify API track.
func (e *Engine) pick(now time.Time) *player.Track {
	cfg := e.Cfg.Get()
	e.mu.Lock()
	sessions := make([]player.Track, len(e.lastSMTCSessions))
	copy(sessions, e.lastSMTCSessions)
	sp := e.spotify
	e.mu.Unlock()
	if sp != nil && time.Since(sp.LastUpdated) < 30*time.Second {
		sessions = append(sessions, *sp)
	} else if sp != nil && time.Since(sp.LastUpdated) >= 30*time.Second {
		e.mu.Lock()
		e.spotify = nil
		e.mu.Unlock()
	}
	return player.Arbitrate(sessions, cfg.Sources.Order, cfg.Sources.Blocked)
}

func (e *Engine) step(now time.Time) {
	cfg := e.Cfg.Get()
	cur := e.pick(now)
	id := identityOf(cur)
	e.mu.Lock()
	changed := identityOf(e.cur) != id
	e.cur = cur
	if changed {
		e.artGeneration++
	}
	identityChanged := id != e.lastIdentity
	if identityChanged {
		e.lastIdentity = id
		e.identityAt = now
		e.lastArtURL = ""
		e.lastListenURL = ""
		e.lastVia = ""
	}
	e.mu.Unlock()
	if changed {
		e.writeState()
	}

	if cur == nil {
		e.mu.Lock()
		revoked := e.publishedTrack != nil && player.IsBlocked(*e.publishedTrack, cfg.Sources.Blocked)
		lastSend := e.lastSend
		e.mu.Unlock()
		if revoked {
			if lastSend.IsZero() || now.Sub(lastSend) >= time.Duration(cfg.Presence.MinUpdateSecs)*time.Second {
				e.clear(now, "")
			}
			return
		}
		idle := cfg.Presence.IdleClearSecs
		if !e.lastActivity.IsZero() && time.Since(e.lastActivity) > time.Duration(idle)*time.Second {
			if !e.cleared {
				e.clear(now, "")
			}
		} else if e.ipcAlive() {
			_ = e.ipc.Ping()
		}
		return
	}
	e.lastActivity = now

	// Debounce rapid track changes (fast Next-Next skips).
	if identityChanged {
		e.resolveArtAsync(*cur, cfg.Art.Prefer, id)
		return
	}
	if now.Sub(e.identityAt) < time.Duration(cfg.Presence.TrackDebounceMS)*time.Millisecond {
		return
	}

	// Discord cadence: one update per min_update_secs.
	minGap := time.Duration(cfg.Presence.MinUpdateSecs) * time.Second
	if !e.lastSend.IsZero() && now.Sub(e.lastSend) < minGap {
		return
	}

	// Send only when identity, artwork, visibility policy, or drift changes.
	sameTrack := id == e.sentID
	drift := false
	if sameTrack && cur.Playing && !e.sentElapsedAt.IsZero() {
		shownElapsed := now.Sub(e.sentElapsedAt).Seconds() + e.sentElapsedSec
		actual := cur.ElapsedSec(now)
		drift = absDiff(shownElapsed, actual) > float64(cfg.Presence.DriftResyncSecs)
	}
	wantsClear := !cur.Playing && (cfg.Presence.PausedMode == "hide" || cfg.Presence.PausedMode == "clear")
	e.mu.Lock()
	artChanged := discordAssetURL(e.lastArtURL) != e.sentArtURL
	visibilityChanged := e.cleared != wantsClear
	e.mu.Unlock()
	if sameTrack && !drift && !artChanged && !visibilityChanged {
		return
	}
	e.send(cur, now)
}

// identityOf keys a track pointer for change detection ("" for none).
func identityOf(t *player.Track) string {
	if t == nil {
		return ""
	}
	return t.IdentityPlaying()
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

func activityType(cfgType string) int {
	switch cfgType {
	case "playing":
		return discordipc.TypePlaying
	case "watching":
		return discordipc.TypeWatching
	default:
		return discordipc.TypeListening
	}
}

func statusDisplayType(s string) int {
	switch s {
	case "name":
		return 0
	case "state":
		return 1
	case "details":
		return 2
	default:
		return 0
	}
}

func (e *Engine) ipcAlive() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ipc != nil && e.ipc.Alive()
}

func (e *Engine) ensureIPC(clientID string) bool {
	e.mu.Lock()
	if e.ipc != nil && e.ipc.Alive() {
		e.mu.Unlock()
		return true
	}
	e.mu.Unlock()
	if time.Now().UnixNano() < e.nextDial.Load() {
		return false
	}
	c, err := discordipc.Connect(clientID)
	next := time.Now().Add(10 * time.Second).UnixNano()
	e.nextDial.Store(next)
	if err != nil {
		e.setErr(err.Error())
		return false
	}
	e.mu.Lock()
	e.ipc = c
	e.mu.Unlock()
	log.Printf("discord: connected (client %s)", clientID)
	return true
}

func (e *Engine) disconnect() {
	e.mu.Lock()
	ipc := e.ipc
	e.ipc = nil
	e.mu.Unlock()
	if ipc != nil {
		ipc.Close()
	}
}

func (e *Engine) setErr(s string) {
	e.mu.Lock()
	changed := e.lastErr != s
	e.lastErr = s
	e.mu.Unlock()
	if changed {
		if s != "" {
			log.Printf("presence: %s", s)
		}
		e.writeState()
	}
}

// clear removes the presence from Discord and records the handled selection.
func (e *Engine) clear(now time.Time, identity string) {
	e.mu.Lock()
	if e.cleared {
		e.sentID = identity
		e.sentArtURL = discordAssetURL(e.lastArtURL)
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()
	cfg := e.Cfg.Get()
	if !e.ensureIPC(cfg.Discord.ClientID) {
		return
	}
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()
	if err := ipc.SetActivity(nil); err != nil {
		e.setErr("clear: " + err.Error())
		if strings.Contains(err.Error(), "write timed out") {
			e.nextDial.Store(time.Now().Add(time.Second).UnixNano())
		}
		e.disconnect()
		return
	}
	e.mu.Lock()
	e.cleared = true
	e.sentID = identity
	e.sentArtURL = discordAssetURL(e.lastArtURL)
	e.lastSend = now
	e.published = nil
	e.publishedTrack = nil
	e.acknowledged = ipc.AcceptedPayload()
	e.mu.Unlock()
	e.setErr("")
	e.writeState()
}

// send builds and pushes the SET_ACTIVITY payload for a track.
func (e *Engine) send(t *player.Track, now time.Time) {
	cfg := e.Cfg.Get()
	clientID := cfg.Discord.ClientID
	if override := cfg.Discord.Apps[t.Source]; override != "" {
		clientID = override
	}
	if strings.TrimSpace(clientID) == "" {
		e.setErr("no discord.client_id configured (run `uika-resonance setup`)")
		return
	}
	if !e.ensureIPC(clientID) {
		return
	}

	appTmpl := template.New(cfg.Template.Details, cfg.Template.State, cfg.Template.LargeText, cfg.Template.SmallText, cfg.Template.Paused)
	details, state, largeText, _ := appTmpl.Render(*t, now)

	if !t.Playing {
		switch cfg.Presence.PausedMode {
		case "hide", "clear":
			e.clear(now, t.IdentityPlaying())
			return
		}
	}

	a := buildActivity(e.activityName(cfg, t), activityType(cfg.Presence.Type), statusDisplayType(cfg.Presence.StatusDisplay), details, state, t, now)

	e.mu.Lock()
	ipc := e.ipc
	imageURL := e.lastArtURL
	listenURL := e.lastListenURL
	e.mu.Unlock()
	// The album stays large; the app badge is always the small image.
	// Loopback cache URLs are dashboard-only; Discord cannot fetch them.
	assetImage := discordAssetURL(imageURL)
	a.Assets = &discordipc.Assets{LargeImage: assetImage, LargeText: largeText, SmallImage: cfg.Discord.SmallImage, SmallText: cfg.Discord.AppName}
	if cfg.Buttons.Enabled {
		if target := musicURL(t, listenURL, cfg.Buttons.SpotifySearch); target != "" {
			label := cfg.Buttons.Label
			if label == "" || label == "Listen along" {
				label = "Listen on " + serviceName(t)
			}
			a.Buttons = append(a.Buttons, discordipc.Button{Label: template.Truncate(label, template.MaxButtonLabel), URL: target})
		}
		if validWebURL(cfg.Buttons.YozoraURL) {
			a.Buttons = append(a.Buttons, discordipc.Button{Label: "Yozora", URL: cfg.Buttons.YozoraURL})
		}
	}
	if err := ipc.SetActivity(a); err != nil {
		e.setErr("send: " + err.Error())
		if strings.Contains(err.Error(), "write timed out") {
			e.nextDial.Store(time.Now().Add(time.Second).UnixNano())
		}
		e.disconnect()
		return
	}
	identity := t.IdentityPlaying()
	e.mu.Lock()
	e.sentID = identity
	e.sentArtURL = assetImage
	e.published = a
	e.publishedTrack = &player.Track{Source: t.Source, AppID: t.AppID}
	e.acknowledged = ipc.AcceptedPayload()
	e.sentElapsedAt = now
	e.sentElapsedSec = t.ElapsedSec(now)
	e.lastSend = now
	e.sends++
	e.cleared = false
	e.mu.Unlock()
	e.setErr("")
	e.writeState()
}

func buildActivity(name string, activityType, displayType int, details, state string, t *player.Track, now time.Time) *discordipc.Activity {
	a := &discordipc.Activity{
		Name:              name,
		Type:              activityType,
		StatusDisplayType: displayType,
		Details:           details,
		State:             state,
	}
	if t.Playing && t.DurationSec > 0 {
		start := now.Unix() - int64(t.ElapsedSec(now))
		end := start + int64(t.DurationSec)
		a.Timestamps = &discordipc.Timestamps{Start: start, End: end}
	}
	return a
}

func (e *Engine) resolveArtAsync(t player.Track, prefer, identity string) {
	if e.Art == nil {
		return
	}
	e.mu.Lock()
	generation := e.artGeneration
	e.mu.Unlock()
	go func() {
		lookup := e.Art.Resolve(art.Track{
			Source: t.Source, Title: t.Title, Artist: t.Artist, Album: t.Album,
			ArtURL: t.ArtURL, Thumb: t.Thumb,
		}, prefer)
		imageURL := lookup.ImageURL
		// Preserve the resolved artwork for the local dashboard verbatim
		// (including loopback SMTC thumbnails); the Discord asset stays
		// empty for loopback art (send() re-strips it) or when the lookup
		// failed, since DefaultURL() is itself loopback-hosted and Discord
		// renders its own app-icon fallback for a missing large image.
		dashboardArt := imageURL
		discordArt := discordAssetURL(imageURL)
		if imageURL == "" {
			discordArt = e.Art.DefaultURL()
			dashboardArt = discordArt
		}
		e.mu.Lock()
		current := e.artGeneration == generation && e.cur != nil && e.cur.IdentityPlaying() == identity
		if current {
			e.lastArtURL = dashboardArt
			e.lastListenURL = lookup.ListenURL
			e.lastVia = lookup.Via
		}
		e.mu.Unlock()
		if current {
			e.writeState()
		}
	}()
}

func (e *Engine) activityName(cfg *config.Config, t *player.Track) string {
	if n := cfg.Discord.Names[t.Source]; n != "" {
		return n
	}
	return serviceName(t)
}

// Status snapshots the current engine state.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := Status{
		UpdatedAt:    time.Now(),
		Connected:    e.ipc != nil && e.ipc.Alive(),
		LastSend:     e.lastSend,
		LastErr:      e.lastErr,
		Sends:        e.sends,
		ArtURL:       e.lastArtURL,
		Via:          e.lastVia,
		Published:    e.published,
		Acknowledged: e.acknowledged,
	}
	if e.Cfg != nil {
		s.SmallImage = e.Cfg.Get().Discord.SmallImage
	}
	if e.cur != nil {
		now := time.Now()
		s.Playing = e.cur.Playing
		s.Source = e.cur.Source
		s.Service = serviceName(e.cur)
		s.Title = e.cur.Title
		s.Artist = e.cur.Artist
		s.Album = e.cur.Album
		s.Elapsed = e.cur.ElapsedSec(now)
		s.Duration = e.cur.DurationSec
	}
	return s
}

// writeState persists the status for the `status` command.
func (e *Engine) writeState() {
	if e.StateDir == "" {
		return
	}
	// Artwork completion and the engine can both persist a snapshot.
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	s := e.Status()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	final := filepath.Join(e.StateDir, "state.json")
	tmp := final + ".tmp"
	if err := os.MkdirAll(e.StateDir, 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, final)
}

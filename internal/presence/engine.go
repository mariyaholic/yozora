//go:build windows

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
	"uika-resonance/internal/browserwin"
	"uika-resonance/internal/config"
	"uika-resonance/internal/discordipc"
	"uika-resonance/internal/player"
	"uika-resonance/internal/smtc"
	"uika-resonance/internal/template"
)

type Status struct {
	ActivityType string               `json:"activity_type"`
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

type browserClassification struct{ Service, VideoID string }

type Engine struct {
	EnumerateWindows func() []browserwin.Window
	browserCache     map[string]browserClassification
	classifyPending  bool
	Cfg              *config.Holder
	Art              artworkResolver
	StateDir         string

	smtcCh    <-chan []smtc.Track
	spotifyCh <-chan player.Track

	mu                sync.Mutex
	stateMu           sync.Mutex
	ipc               activityClient
	cur               *player.Track
	spotify           *player.Track
	lastSMTCSessions  []player.Track
	identityAt        time.Time
	lastIdentity      string
	lastSend          time.Time
	sentID            string
	sentElapsedAt     time.Time
	sentElapsedSec    float64
	lastActivity      time.Time
	cleared           bool
	nextDial          atomic.Int64
	sends             int64
	lastErr           string
	lastArtURL        string
	lastDiscordArtURL string
	lastListenURL     string
	lastVia           string
	artGeneration     uint64
	sentArtURL        string
	published         *discordipc.Activity
	publishedTrack    *player.Track
	acknowledged      json.RawMessage
}

func New(cfg *config.Holder, a *art.Resolver, smtcCh <-chan []smtc.Track, spotifyCh <-chan player.Track, stateDir string) *Engine {
	return &Engine{
		Cfg: cfg, Art: a,
		smtcCh: smtcCh, spotifyCh: spotifyCh,
		StateDir: stateDir,
	}
}

func (e *Engine) Run(ctx context.Context) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()

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

func (e *Engine) pick(now time.Time) *player.Track {
	cfg := e.Cfg.Get()
	e.mu.Lock()
	sessions := make([]player.Track, len(e.lastSMTCSessions))
	copy(sessions, e.lastSMTCSessions)
	for i := range sessions {
		if sessions[i].Source == "browser" {
			c := e.browserCache[sessions[i].Identity()]
			sessions[i].Service, sessions[i].VideoID = c.Service, c.VideoID
		}
	}
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
		e.lastDiscordArtURL = ""
		e.lastListenURL = ""
		e.lastVia = ""
	}
	e.mu.Unlock()
	if changed {
		e.writeState()
	}

	e.mu.Lock()
	if e.publishedTrack != nil && e.publishedTrack.Source == "browser" {
		if c, ok := e.browserCache[e.publishedTrack.Identity()]; ok {
			e.publishedTrack.Service = c.Service
		}
	}
	e.mu.Unlock()
	e.mu.Lock()
	revoked := e.publishedTrack != nil && player.IsBlocked(*e.publishedTrack, cfg.Sources.Blocked)
	lastSend := e.lastSend
	e.mu.Unlock()
	if cur == nil && revoked {
		if lastSend.IsZero() || now.Sub(lastSend) >= time.Duration(cfg.Presence.MinUpdateSecs)*time.Second {
			e.clear(now, "")
		}
		return
	}
	if cur == nil {
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

	e.mu.Lock()
	_, classified := e.browserCache[cur.Identity()]
	needsClassification := e.Art != nil && cur.Source == "browser" && !classified && !e.classifyPending
	e.mu.Unlock()
	if identityChanged || needsClassification {
		// A browser replacement is about to be classified; do not leave a revoked card up meanwhile.
		if revoked && cur.Source == "browser" && (lastSend.IsZero() || now.Sub(lastSend) >= time.Duration(cfg.Presence.MinUpdateSecs)*time.Second) {
			e.clear(now, "")
		}
		e.resolveArtAsync(*cur, cfg.Art.Prefer, id)
		return
	}
	e.mu.Lock()
	classification := e.browserCache[cur.Identity()]
	pending := cur.Source == "browser" && (e.classifyPending || classification.Service != cur.Service || classification.VideoID != cur.VideoID)
	e.mu.Unlock()
	if pending {
		// A replacement awaiting browser classification cannot safely be published yet.
		if revoked && (lastSend.IsZero() || now.Sub(lastSend) >= time.Duration(cfg.Presence.MinUpdateSecs)*time.Second) {
			e.clear(now, "")
		}
		return
	}
	if now.Sub(e.identityAt) < time.Duration(cfg.Presence.TrackDebounceMS)*time.Millisecond {
		return
	}

	minGap := time.Duration(cfg.Presence.MinUpdateSecs) * time.Second
	if !e.lastSend.IsZero() && now.Sub(e.lastSend) < minGap {
		return
	}

	sameTrack := id == e.sentID
	drift := false
	if sameTrack && cur.Playing && !e.sentElapsedAt.IsZero() {
		shownElapsed := now.Sub(e.sentElapsedAt).Seconds() + e.sentElapsedSec
		actual := cur.ElapsedSec(now)
		drift = absDiff(shownElapsed, actual) > float64(cfg.Presence.DriftResyncSecs)
	}
	wantsClear := !cur.Playing && (cfg.Presence.PausedMode == "hide" || cfg.Presence.PausedMode == "clear")
	e.mu.Lock()
	artChanged := discordAssetURL(e.lastDiscordArtURL) != e.sentArtURL
	visibilityChanged := e.cleared != wantsClear
	e.mu.Unlock()
	if sameTrack && !drift && !artChanged && !visibilityChanged {
		return
	}
	e.send(cur, now)
}

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

func trackActivityType(t *player.Track, cfgType string) string {
	if t.Service == "youtube" {
		return "watching"
	}
	if t.Source == "youtubemusic" {
		return "listening"
	}
	switch cfgType {
	case "watching", "playing":
		return cfgType
	default:
		return "listening"
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

func (e *Engine) clear(now time.Time, identity string) {
	e.mu.Lock()
	if e.cleared {
		e.sentID = identity
		e.sentArtURL = discordAssetURL(e.lastDiscordArtURL)
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
	e.sentArtURL = discordAssetURL(e.lastDiscordArtURL)
	e.lastSend = now
	e.published = nil
	e.publishedTrack = nil
	e.acknowledged = ipc.AcceptedPayload()
	e.mu.Unlock()
	e.setErr("")
	e.writeState()
}

func (e *Engine) send(t *player.Track, now time.Time) {
	cfg := e.Cfg.Get()
	if player.IsBlocked(*t, cfg.Sources.Blocked) {
		return
	}
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

	a := buildActivity(e.activityName(cfg, t), activityType(trackActivityType(t, cfg.Presence.Type)), statusDisplayType(cfg.Presence.StatusDisplay), details, state, t, now)

	e.mu.Lock()
	ipc := e.ipc
	imageURL := e.lastDiscordArtURL
	listenURL := e.lastListenURL
	e.mu.Unlock()

	assetImage := discordAssetURL(imageURL)
	a.Assets = &discordipc.Assets{LargeImage: assetImage, LargeText: largeText, SmallImage: cfg.Discord.SmallImage, SmallText: cfg.Discord.AppName}
	if cfg.Buttons.Enabled {
		if target := musicURL(t, listenURL, cfg.Buttons.SpotifySearch); target != "" {
			label := cfg.Buttons.Label
			if label == "" || label == "Listen along" {
				label = "Listen on " + serviceName(t)
				if t.Service == "youtube" {
					label = "Watch on YouTube"
				}
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
	publishedTrack := *t
	publishedTrack.Thumb = nil
	e.publishedTrack = &publishedTrack
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
	e.classifyPending = t.Source == "browser"
	enumerate := e.EnumerateWindows
	if enumerate == nil {
		enumerate = browserwin.Enumerate
	}
	e.mu.Unlock()
	go func() {
		classification := browserClassification{}
		if t.Source == "browser" {
			windows := enumerate()
			titles := make([]string, len(windows))
			for i, w := range windows {
				titles[i] = w.Title
			}
			// Ambiguous YouTube matches must still be classified to prevent blocked browser cards leaking.
			if _, count := browserwin.MatchYouTubeTitle(titles, t.Title); count >= 1 {
				classification.Service = "youtube"
			}
		}
		lookup := e.Art.Resolve(art.Track{
			Source: t.Source, Title: t.Title, Artist: t.Artist, Album: t.Album,
			ArtURL: t.ArtURL, Thumb: t.Thumb,
		}, prefer)
		if classification.Service == "youtube" {
			lookup.DiscordURL = art.YouTubeSymbolURL
			lookup.ListenURL = ""
			if lookup.ImageURL == "" {
				lookup.ImageURL = art.YouTubeSymbolURL
				lookup.Via = "symbol"
			}
		}
		imageURL := lookup.ImageURL

		dashboardArt := imageURL
		discordArt := lookup.DiscordURL
		if discordArt == "" {
			discordArt = discordAssetURL(imageURL)
		}
		if imageURL == "" {
			dashboardArt = e.Art.DefaultURL()
			if discordArt == "" {
				discordArt = discordAssetURL(dashboardArt)
			}
		}
		e.mu.Lock()
		current := e.artGeneration == generation && e.cur != nil && e.cur.IdentityPlaying() == identity
		if current {
			if t.Source == "browser" {
				if e.browserCache == nil || len(e.browserCache) >= 16 {
					e.browserCache = make(map[string]browserClassification)
				}
				e.browserCache[t.Identity()] = classification
				e.classifyPending = false
			}
			e.lastArtURL = dashboardArt
			e.lastDiscordArtURL = discordArt
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
	key := t.Source
	if t.Service == "youtube" {
		key = "youtube"
	}
	if n := cfg.Discord.Names[key]; n != "" {
		return n
	}
	return serviceName(t)
}

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
		cfgType := ""
		if e.Cfg != nil {
			cfgType = e.Cfg.Get().Presence.Type
		}
		s.ActivityType = trackActivityType(e.cur, cfgType)
		s.Title = e.cur.Title
		s.Artist = e.cur.Artist
		s.Album = e.cur.Album
		s.Elapsed = e.cur.ElapsedSec(now)
		s.Duration = e.cur.DurationSec
	}
	return s
}

func (e *Engine) writeState() {
	if e.StateDir == "" {
		return
	}

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

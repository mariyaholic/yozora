//go:build windows

package presence

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
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

type browserClassification struct{ Service, VideoID, Channel string }

type Engine struct {
	EnumerateWindows func() []browserwin.Window
	ReadAddressBar   func(uintptr) (string, error)
	ReadTabNames     func(uintptr, int) ([]string, error)
	browserCache     map[string]browserClassification
	browserPrior     browserClassification
	browserPriorArt  art.Lookup
	classifyPending  bool
	Cfg              *config.Holder
	Art              artworkResolver
	StateDir         string

	smtcCh    <-chan []smtc.Track
	spotifyCh <-chan player.Track

	mu                sync.Mutex
	stateMu           sync.Mutex
	ipc               activityClient
	ipcClientID       string
	dial              func(string) (activityClient, error)
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
	artConfig         string
	sentArtURL        string
	sentConfig        string
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
		case tracks, ok := <-e.smtcCh:
			if !ok {
				e.smtcCh = nil
				e.ingest(nil)
				continue
			}
			e.ingest(tracks)
		case tr, ok := <-e.spotifyCh:
			if !ok {
				e.spotifyCh = nil
				e.mu.Lock()
				e.spotify = nil
				e.mu.Unlock()
				continue
			}
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
			c := e.browserCache[browserIdentity(sessions[i])]
			sessions[i].Service, sessions[i].VideoID, sessions[i].Channel = c.Service, c.VideoID, c.Channel
		}
	}
	sp := e.spotify
	e.mu.Unlock()
	if sp != nil && now.Sub(sp.LastUpdated) < 30*time.Second {
		sessions = append(sessions, *sp)
	} else if sp != nil && now.Sub(sp.LastUpdated) >= 30*time.Second {
		e.mu.Lock()
		e.spotify = nil
		e.mu.Unlock()
	}
	return player.Arbitrate(sessions, cfg.Sources.Order, cfg.Sources.Blocked)
}

func (e *Engine) step(now time.Time) {
	cfg := e.Cfg.Get()
	resolverKey := artworkConfigKey(cfg)
	e.mu.Lock()
	resolverConfigChanged := e.Art != nil && e.artConfig != resolverKey
	if resolverConfigChanged {
		e.artConfig = resolverKey
		e.artGeneration++
		e.classifyPending = false
		// Keep prior classification for source revocation checks until the
		// replacement resolver commits; publishing is held while it runs.
		// Tab-only cache entries may currently suppress selection when YouTube
		// is blocked. Remove those on discovery disable so the unchanged
		// browser item can be reclassified rather than staying stuck at nil.
		tabsDisabled := slices.ContainsFunc(cfg.Sources.Blocked, func(value string) bool {
			return strings.EqualFold(strings.TrimSpace(value), "youtube_tabs")
		})
		if tabsDisabled {
			for key, classification := range e.browserCache {
				if classification.Service == "youtube" && classification.VideoID == "" {
					if e.publishedTrack != nil && browserIdentity(*e.publishedTrack) == key {
						e.publishedTrack.Service = classification.Service
					}
					delete(e.browserCache, key)
				}
			}
		}
		// Only verified video evidence survives discovery-setting changes.
		// A tab-title-only classification must be re-evaluated when disabled.
		if e.browserPrior.VideoID == "" {
			e.browserPrior = browserClassification{}
			e.browserPriorArt = art.Lookup{}
		}
	}
	e.mu.Unlock()
	cur := e.pick(now)
	id := identityOf(cur)
	e.mu.Lock()
	changed := identityOf(e.cur) != id
	// Only retain YouTube evidence for one continuously selected media item.
	// Play/pause and active-tab changes do not change that item. A gap,
	// different source application or different metadata invalidates it.
	if !sameBrowserItem(e.cur, cur) {
		e.browserPrior = browserClassification{}
		e.browserPriorArt = art.Lookup{}
	}
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
		if c, ok := e.browserCache[browserIdentity(*e.publishedTrack)]; ok {
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
		if !e.lastActivity.IsZero() && now.Sub(e.lastActivity) > time.Duration(idle)*time.Second {
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
	_, classified := e.browserCache[browserIdentity(*cur)]
	needsClassification := e.Art != nil && cur.Source == "browser" && !classified && !e.classifyPending
	e.mu.Unlock()
	if identityChanged || needsClassification || resolverConfigChanged {
		// A browser replacement is about to be classified; do not leave a revoked card up meanwhile.
		if revoked && cur.Source == "browser" && (lastSend.IsZero() || now.Sub(lastSend) >= time.Duration(cfg.Presence.MinUpdateSecs)*time.Second) {
			e.clear(now, "")
		}
		e.resolveArtAsync(*cur, cfg.Art.Prefer, id)
		return
	}
	e.mu.Lock()
	classification := e.browserCache[browserIdentity(*cur)]
	pending := cur.Source == "browser" && (e.classifyPending || classification.Service != cur.Service || classification.VideoID != cur.VideoID || classification.Channel != cur.Channel)
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
	configChanged := !wantsClear && e.sentConfig != presentationConfigKey(cfg)
	e.mu.Unlock()
	clientID := discordClientID(cfg, cur)
	e.mu.Lock()
	connected := e.ipc != nil && e.ipc.Alive() && (e.ipcClientID == "" || e.ipcClientID == clientID)
	e.mu.Unlock()
	if sameTrack && !drift && !artChanged && !visibilityChanged && !configChanged && connected {
		return
	}
	e.send(cur, now)
}

func identityOf(t *player.Track) string {
	if t == nil {
		return ""
	}
	id := t.IdentityPlaying()
	if t.Source == "browser" && t.AppID != "" {
		id += "\x00" + t.AppID
	}
	return id
}

func browserIdentity(t player.Track) string {
	id := t.Identity()
	if t.AppID != "" {
		id += "\x00" + t.AppID
	}
	return id
}

func sameBrowserItem(a, b *player.Track) bool {
	return a != nil && b != nil && a.Source == "browser" && b.Source == "browser" && browserIdentity(*a) == browserIdentity(*b)
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
	if t.Service == "youtube" || t.Service == "twitch" {
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

func (e *Engine) ensureIPC(clientID string, now time.Time) bool {
	e.mu.Lock()
	old := e.ipc
	if old != nil && old.Alive() && (e.ipcClientID == "" || e.ipcClientID == clientID) {
		e.mu.Unlock()
		return true
	}
	changedClient := e.ipcClientID != "" && e.ipcClientID != clientID
	e.ipc = nil
	// Remember the attempted application even when dial fails. Its backoff
	// must not block a different source, and a later clear must target it.
	e.ipcClientID = clientID
	e.mu.Unlock()
	if old != nil {
		old.Close()
	}
	// A source-specific client ID is a new connection, not a retry of the
	// previous ID. Do not delay it behind the old connection's dial backoff.
	if changedClient {
		e.nextDial.Store(0)
	}
	if now.UnixNano() < e.nextDial.Load() {
		return false
	}
	dial := e.dial
	if dial == nil {
		dial = func(id string) (activityClient, error) { return discordipc.Connect(id) }
	}
	c, err := dial(clientID)
	next := now.Add(10 * time.Second).UnixNano()
	e.nextDial.Store(next)
	if err != nil {
		e.setErr(err.Error())
		return false
	}
	e.mu.Lock()
	e.ipc = c
	e.ipcClientID = clientID
	// Publication acknowledgments belong to the old connection.
	e.cleared = false
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
	e.mu.Lock()
	clientID := e.ipcClientID
	e.mu.Unlock()
	if clientID == "" {
		clientID = cfg.Discord.ClientID
	}
	if !e.ensureIPC(clientID, now) {
		return
	}
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()
	if err := ipc.SetActivity(nil); err != nil {
		e.setErr("clear: " + err.Error())
		if strings.Contains(err.Error(), "write timed out") {
			e.nextDial.Store(now.Add(time.Second).UnixNano())
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

// Artwork preferences and browser discovery toggles apply to the current item,
// not just the next track. Normalize ordering/case to avoid redundant restarts.
func artworkConfigKey(cfg *config.Config) string {
	blocked := make([]string, len(cfg.Sources.Blocked))
	for i, value := range cfg.Sources.Blocked {
		blocked[i] = strings.ToLower(strings.TrimSpace(value))
	}
	slices.Sort(blocked)
	key, _ := json.Marshal(struct {
		Prefer  string
		Blocked []string
	}{cfg.Art.Prefer, blocked})
	return string(key)
}

// presentationConfigKey captures only settings used to construct a Discord
// activity. A live config reload must refresh an unchanged track as well.
func presentationConfigKey(cfg *config.Config) string {
	key, _ := json.Marshal(struct {
		Discord                         config.Discord
		Type, StatusDisplay, PausedMode string
		Template                        config.Template
		Buttons                         config.Buttons
		Artwork                         string
	}{cfg.Discord, cfg.Presence.Type, cfg.Presence.StatusDisplay, cfg.Presence.PausedMode, cfg.Template, cfg.Buttons, artworkConfigKey(cfg)})
	return string(key)
}

func discordClientID(cfg *config.Config, t *player.Track) string {
	if override := cfg.Discord.Apps[t.Source]; override != "" {
		return override
	}
	return cfg.Discord.ClientID
}

func (e *Engine) send(t *player.Track, now time.Time) {
	cfg := e.Cfg.Get()
	if player.IsBlocked(*t, cfg.Sources.Blocked) {
		return
	}
	clientID := discordClientID(cfg, t)
	if strings.TrimSpace(clientID) == "" {
		e.setErr("no discord.client_id configured (run `Yozora.exe setup`)")
		return
	}
	if !e.ensureIPC(clientID, now) {
		return
	}

	appTmpl := template.New(cfg.Template.Details, cfg.Template.State, cfg.Template.LargeText, cfg.Template.SmallText, cfg.Template.Paused)
	details, state, largeText, _ := appTmpl.Render(*t, now)

	if !t.Playing {
		switch cfg.Presence.PausedMode {
		case "hide", "clear":
			e.clear(now, identityOf(t))
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
				switch t.Service {
				case "youtube":
					label = "Watch on YouTube"
				case "twitch":
					label = "Watch on Twitch"
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
			e.nextDial.Store(now.Add(time.Second).UnixNano())
		}
		e.disconnect()
		return
	}
	identity := identityOf(t)
	e.mu.Lock()
	e.sentID = identity
	e.sentArtURL = assetImage
	e.sentConfig = presentationConfigKey(cfg)
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
	resolverKey := artworkConfigKey(e.Cfg.Get())
	previous, previousArt := e.browserPrior, e.browserPriorArt
	e.classifyPending = t.Source == "browser"
	enumerate := e.EnumerateWindows
	if enumerate == nil {
		enumerate = browserwin.Enumerate
	}
	readAddress := e.ReadAddressBar
	if readAddress == nil {
		readAddress = browserwin.ReadAddressBar
	}
	readTabs := e.ReadTabNames
	if readTabs == nil {
		readTabs = browserwin.ReadTabNames
	}
	tabsEnabled := !slices.ContainsFunc(e.Cfg.Get().Sources.Blocked, func(blocked string) bool {
		return strings.ToLower(strings.TrimSpace(blocked)) == "youtube_tabs"
	})
	e.mu.Unlock()
	go func() {
		classification := browserClassification{}
		activeMatch := false
		retained := false
		if t.Source == "browser" {
			windows := enumerate()
			titles := make([]string, len(windows))
			for i, w := range windows {
				titles[i] = w.Title
			}
			// Ambiguous YouTube matches must still be classified to prevent blocked browser cards leaking.
			if index, count := browserwin.MatchYouTubeTitle(titles, t.Title); count >= 1 {
				activeMatch = true
				classification.Service = "youtube"
				candidate := t
				candidate.Service = "youtube"
				// The address bar is read only for a unique match while YouTube is enabled.
				if count == 1 && !player.IsBlocked(candidate, e.Cfg.Get().Sources.Blocked) {
					hwnd := windows[index].HWND
					if raw, err := readAddress(hwnd); err == nil {
						if id, valid := browserwin.ParseYouTubeWatchID(raw); valid {
							again := enumerate()
							titles = titles[:0]
							for _, w := range again {
								titles = append(titles, w.Title)
							}
							if i, n := browserwin.MatchYouTubeTitle(titles, t.Title); n == 1 && again[i].HWND == hwnd {
								classification.VideoID = id
							}
						}
					}
				}
			} else if index, count := browserwin.MatchTwitchTitle(titles, t.Title, t.Artist); count >= 1 {
				activeMatch = true
				// Same rules as YouTube: ambiguous matches still classify, so a blocked Twitch card cannot leak.
				classification.Service = "twitch"
				candidate := t
				candidate.Service = "twitch"
				if count == 1 && !player.IsBlocked(candidate, e.Cfg.Get().Sources.Blocked) {
					hwnd := windows[index].HWND
					if raw, err := readAddress(hwnd); err == nil {
						if login, valid := browserwin.ParseTwitchChannel(raw); valid {
							again := enumerate()
							titles = titles[:0]
							for _, w := range again {
								titles = append(titles, w.Title)
							}
							if i, n := browserwin.MatchTwitchTitle(titles, t.Title, t.Artist); n == 1 && again[i].HWND == hwnd {
								classification.Channel = login
							}
						}
					}
				}
			} else if tabsEnabled {
				remaining := 200
				for i, window := range windows {
					if i >= 8 || remaining <= 0 {
						break
					}
					names, err := readTabs(window.HWND, remaining)
					if len(names) > remaining {
						names = names[:remaining]
					}
					remaining -= len(names)
					if err == nil && browserwin.MatchYouTubeTabName(names, t.Title) {
						classification.Service = "youtube"
						break
					}
				}
			}
		}

		// A background tab's address bar belongs to the foreground tab. Do not
		// read it or guess a URL. Reuse only evidence from this same continuous
		// media item when no current foreground match contradicts it.
		if t.Source == "browser" && !activeMatch && previous.Service == "youtube" {
			classification = previous
			retained = true
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
		if classification.Service == "twitch" {
			lookup.DiscordURL = art.TwitchSymbolURL
			lookup.ListenURL = ""
			if lookup.ImageURL == "" {
				lookup.ImageURL = art.TwitchSymbolURL
				lookup.Via = "symbol"
			}
		}
		if classification.Channel != "" {
			image, via := art.TwitchSymbolURL, "symbol"
			if previews, ok := e.Art.(interface{ TwitchThumbnail(string) (string, string) }); ok {
				image, via = previews.TwitchThumbnail(classification.Channel)
			}
			lookup.ImageURL, lookup.DiscordURL, lookup.Via = image, image, via
		}
		if classification.VideoID != "" {
			image, via := art.YouTubeSymbolURL, "symbol"
			if retained && previousArt.Via == "youtube" && previousArt.DiscordURL != "" {
				image, via = previousArt.DiscordURL, previousArt.Via
			} else if thumbnails, ok := e.Art.(interface{ YouTubeThumbnail(string) (string, string) }); ok {
				image, via = thumbnails.YouTubeThumbnail(classification.VideoID)
			}
			lookup.ImageURL, lookup.DiscordURL, lookup.Via = image, image, via
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
		current := e.artGeneration == generation && artworkConfigKey(e.Cfg.Get()) == resolverKey && e.cur != nil && identityOf(e.cur) == identity
		if current {
			if t.Source == "browser" {
				if e.browserCache == nil || len(e.browserCache) >= 16 {
					e.browserCache = make(map[string]browserClassification)
				}
				e.browserCache[browserIdentity(t)] = classification
				e.browserPrior, e.browserPriorArt = classification, lookup
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
	if t.Service == "youtube" || t.Service == "twitch" {
		key = t.Service
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

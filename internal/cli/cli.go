//go:build windows

package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"uika-resonance/internal/art"
	"uika-resonance/internal/config"
	"uika-resonance/internal/credman"
	"uika-resonance/internal/discordipc"
	"uika-resonance/internal/logfile"
	"uika-resonance/internal/presence"
	"uika-resonance/internal/server"
	"uika-resonance/internal/smtc"
	"uika-resonance/internal/spotifyapi"
	"uika-resonance/internal/sysutil"
	"uika-resonance/internal/update"
	"uika-resonance/internal/version"
)

const logMaxBytes = 5 << 20

// logOut is the rotating log file, nil if it could not be opened.
var logOut *logfile.Writer

// setupLogging opens the log before the config is read, so startup problems are
// recorded. The file rotates whenever it passes logMaxBytes, keeping one old file.
func setupLogging(dataDir string, level string) {
	dir := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(dir, 0o755)
	if w, err := logfile.Open(filepath.Join(dir, "uika-resonance.log"), logMaxBytes); err == nil {
		logOut = w
	}
	applyLogLevel(level)
}

// applyLogLevel applies the log.level setting once the config is loaded.
func applyLogLevel(level string) {
	lv, known := logfile.ParseLevel(level)
	flags := log.LstdFlags | log.LUTC
	if lv.Source {
		flags |= log.Lmicroseconds | log.Lshortfile
	}
	log.SetFlags(flags)
	switch {
	case lv.File && logOut != nil:
		log.SetOutput(io.MultiWriter(logOut, os.Stderr))
	default:
		log.SetOutput(os.Stderr)
		if logOut != nil {
			// Closed, not dropped: a later write reopens it.
			_ = logOut.Close()
		}
	}
	if !known {
		log.Printf("log: unknown level %q, using info (choose one of %s)", level, strings.Join(logfile.Levels, ", "))
	}
}

// watchLogLevel re-applies log.level whenever the config reloads with a change. Reload covers
// both the file watcher and panel saves, so this needs no polling of its own.
func watchLogLevel(h *config.Holder) {
	h.OnChange(func(c *config.Config) { applyLogLevel(c.Log.Level) })
}

// artCountry is the iTunes storefront: [art] country if set, else the Windows region.
func artCountry(cfg *config.Config) string {
	if c := art.CountryCode(cfg.Art.Country); c != "" {
		return c
	}
	return art.CountryCode(sysutil.UserRegion())
}

func tokenOf() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate dashboard token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func Main(args []string) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		return serve()
	case "panel":
		// Not in usage: the daemon's tray runs this to open the control panel.
		return trayPanel()
	case "setup":
		return setup()
	case "status":
		return status()
	case "doctor":
		return doctor()
	case "install":
		if err := sysutil.InstallAutostart(); err != nil {
			log.Println(err)
			return 1
		}
		fmt.Println("autostart installed (HKCU Run)")
		return 0
	case "uninstall":
		if err := sysutil.RemoveAutostart(); err != nil {
			log.Println(err)
			return 1
		}
		fmt.Println("autostart removed")
		return 0
	case "stop":
		_, dataDir, _ := config.Paths()
		if err := sysutil.StopRunning(dashboardEndpointPath(dataDir), 5*time.Second); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Yozora stopped")
		return 0
	case "version", "--version":
		fmt.Println("Yozora", version.Version)
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		fmt.Print(usage)
		return 2
	}
}

const usage = `Yozora — your music, live on your Discord profile.

Usage: Yozora.exe [serve|setup|status|doctor|install|uninstall|stop|version]

  serve        run the daemon (default)
  setup        interactive first-run setup
  status       print the current presence state
  doctor       diagnose pipes, media, art, config
  install      register autostart (HKCU Run)
  uninstall    remove autostart
  stop         quit a running Yozora, as the tray's Quit does
  version      print the version
`

func serve() int {
	cfgPath, dataDir, cacheDir := config.Paths()
	setupLogging(dataDir, "info")
	capRuntimeMemory()
	ok, done, err := sysutil.AcquireSingleInstance()
	if err != nil {
		log.Printf("single-instance mutex: %v", err)
		return 1
	}
	if !ok {
		log.Println("another instance is running; exiting")
		return 1
	}
	defer done()
	if rewrote, err := sysutil.RepairAutostart(); err != nil {
		log.Printf("autostart: %v", err)
	} else if rewrote {
		log.Println("autostart: repointed at this executable")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Println(err)
	}

	holder, err := config.NewHolder(cfgPath)
	if err != nil {
		log.Printf("config: %v", err)
		return 1
	}
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		_ = config.Save(holder.Get(), cfgPath)
		log.Printf("config: wrote starter config to %s", cfgPath)
	}
	cfg := holder.Get()
	applyLogLevel(cfg.Log.Level)
	watchLogLevel(holder)

	res := art.New(cacheDir, int64(cfg.Art.CacheMB)<<20)
	res.SetCountry(artCountry(cfg))
	holder.OnChange(func(c *config.Config) { res.SetCountry(artCountry(c)) })
	base, err := res.StartServer(cfg.Art.Port)
	if err != nil {
		log.Printf("art server: %v (SMTC art disabled)", err)
	} else {
		log.Printf("art server: %s", base)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go holder.Watch(ctx.Done())

	w := smtc.NewWatcher(time.Duration(cfg.Sources.PollMS) * time.Millisecond)
	w.OnError(func(err error) { log.Printf("smtc: %v", err) })
	go w.Run(ctx)

	// The panel can connect or disconnect the Web API later, so the poller lives behind a manager.
	spotify := spotifyapi.NewManager()
	if cfg.Spotify.Mode == "api" && cfg.Spotify.ClientID != "" {
		spotify.Start(cfg.Spotify.ClientID, cfg.Spotify.Market, time.Duration(cfg.Spotify.PollSecs)*time.Second)
		log.Printf("spotify: api mode (poll %ds)", cfg.Spotify.PollSecs)
	}
	go func() { <-ctx.Done(); spotify.Stop() }()

	eng := presence.New(holder, res, w.Out, spotify.Out(), dataDir)

	// One timer, asleep between checks; a dev build never starts it. Looking
	// is automatic (and can be turned off), installing never is.
	upd := update.New(version.Version, filepath.Join(dataDir, "update.json"), func() bool { return holder.Get().Update.Check })
	if upd.Active() {
		go upd.Run(ctx)
	}

	panelLive := false
	if cfg.Server.Enabled {
		tok, tokenErr := tokenOf()
		if tokenErr != nil {
			log.Printf("dashboard: %v", tokenErr)
		} else if base, err := server.Start(server.Deps{
			Engine: eng, Cfg: holder, CfgPath: cfgPath, Token: tok,
			DiscordApp: cfg.Discord.AppName,
			Spotify:    spotify, SpotifyLink: &spotifyapi.Link{}, OpenURL: openURL, Update: upd,
		}, cfg.Server.Port); err == nil {
			log.Printf("dashboard: %s (session token redacted)", base)
			endpointPath := dashboardEndpointPath(dataDir)
			if err := writeDashboardEndpoint(endpointPath, dashboardEndpoint{BaseURL: base, Token: tok, PID: os.Getpid()}); err != nil {
				log.Printf("dashboard: endpoint file: %v", err)
			} else {
				defer os.Remove(endpointPath)
			}
			fmt.Printf("Dashboard URL (session token; do not share): %s?t=%s\n", base, tok)
			panelLive = true
		} else {
			log.Printf("dashboard: %v", err)
		}
	}

	// The tray comes up only after the dashboard is listening and its endpoint
	// is written, so "Open control panel" never races the server's start.
	// Deferred after the endpoint cleanup, so it runs first: the icon goes
	// away before the endpoint file does.
	closeTray := startDaemonTray(holder, stop, dataDir, panelLive, eng, upd)
	defer closeTray()

	log.Printf("Yozora serving — config: %s", cfgPath)
	eng.Run(ctx)
	log.Println("bye")
	return 0
}

func status() int {
	_, dataDir, _ := config.Paths()
	b, err := os.ReadFile(filepath.Join(dataDir, "state.json"))
	if err != nil {
		fmt.Println("no state yet — is the daemon running? (serve, then check again)")
		return 1
	}
	var s presence.Status
	if err := json.Unmarshal(b, &s); err != nil {
		fmt.Println("bad state file:", err)
		return 1
	}
	fmt.Printf("discord: %v  playing: %v  source: %s\n", s.Connected, s.Playing, s.Source)
	fmt.Printf("title:   %s\nartist:  %s\nalbum:   %s\n", s.Title, s.Artist, s.Album)
	fmt.Printf("time:    %s / %s\n", fmtSec(s.Elapsed), fmtSec(s.Duration))
	if !s.LastSend.IsZero() {
		fmt.Printf("sent:    %s (%d sends)\n", s.LastSend.Format(time.RFC3339), s.Sends)
	}
	if s.LastErr != "" {
		fmt.Printf("error:   %s\n", s.LastErr)
	}
	return 0
}

func fmtSec(s float64) string {
	t := int(s + 0.5)
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

func doctor() int {
	cfgPath, dataDir, cacheDir := config.Paths()
	fmt.Println("== Yozora doctor ==")
	fmt.Println("version:", version.Version)
	fmt.Println("config:", cfgPath)
	if _, err := os.Stat(cfgPath); err == nil {
		if _, err := config.Load(cfgPath); err != nil {
			fmt.Println("  FAIL config parse:", err)
		} else {
			fmt.Println("  ok")
		}
	} else {
		fmt.Println("  (missing — defaults in use)")
	}
	cfg, _ := config.Load(cfgPath)
	if cfg.Discord.ClientID == "" {
		fmt.Println("discord client_id: MISSING (run `Yozora.exe setup`)")
	} else {
		fmt.Printf("discord client_id: %s…\n", cfg.Discord.ClientID[:min(6, len(cfg.Discord.ClientID))])
	}
	fmt.Print("discord pipes: ")
	if t, err := discordipc.Dial(); err == nil {
		fmt.Println("found (Discord running)")
		t.Close()
	} else {
		fmt.Println("none — start Discord")
	}
	fmt.Print("smtc: ")
	m, err := smtc.Connect()
	if err != nil {
		fmt.Println("FAIL", err)
	} else {
		trs, err := m.Sessions()
		if err != nil {
			fmt.Println("FAIL", err)
		} else {
			fmt.Printf("ok, %d session(s)\n", len(trs))
			for _, t := range trs {
				fmt.Printf("  [%s] %s — %s (%s)\n", smtc.FriendlyName(t.AppID), t.Artist, t.Title, t.StatusName())
			}
		}
		m.Close()
	}
	fmt.Print("itunes search: ")
	if l := probeItunes(); l.ImageURL != "" {
		fmt.Println("ok", l.ImageURL[:min(60, len(l.ImageURL))]+"…")
	} else {
		fmt.Println("FAIL (offline?)")
	}
	fmt.Printf("art cache: %s\n", cacheDir)
	fmt.Printf("data dir:  %s\n", dataDir)
	fmt.Printf("autostart: %v\n", sysutil.AutostartEnabled())
	if spotifyapi.HasRefreshToken() {
		fmt.Println("spotify: refresh token stored (api mode available)")
	} else {
		fmt.Println("spotify: system mode (no Web API token stored)")
	}
	return 0
}

func probeItunes() art.Lookup {
	r := art.New(filepath.Join(os.TempDir(), "uika-probe"), 1<<20)
	if base, err := r.StartServer(0); err == nil {
		_ = base
	}
	return r.Resolve(art.Track{Source: "probe", Title: "Bohemian Rhapsody", Artist: "Queen"}, "cdn")
}

func setup() int {
	cfgPath, _, _ := config.Paths()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Println("config error:", err)
		return 1
	}
	rd := bufio.NewReader(os.Stdin)
	fmt.Println("== Yozora setup ==")
	fmt.Println("1) Discord Rich Presence is preconfigured for Yozora.")
	fmt.Println("   Press Enter to keep it, or enter your own Application ID to use a different app.")
	prompt := func(label, def string) string {
		fmt.Printf("%s [%s]: ", label, def)
		line, _ := rd.ReadString('\n')
		line = trim(line)
		if line == "" {
			return def
		}
		return line
	}
	cfg.Discord.ClientID = prompt("Discord Application ID (Enter keeps Yozora)", cfg.Discord.ClientID)
	if trim(cfg.Discord.ClientID) == "" {
		fmt.Println("client_id is required for presence to appear.")
		return 1
	}
	fmt.Println()
	fmt.Println("2) Optional: Spotify Web API for richer data (track links, exact art).")
	fmt.Println("   Needs your own Spotify developer app (client ID only, no secret). Leave blank to skip.")
	fmt.Printf("   Add this Redirect URI to the app first: http://127.0.0.1:%d%s\n", setupSpotifyPort, setupSpotifyPath)
	if cid := trim(prompt("Spotify client ID", "")); cid != "" {
		if !spotifyapi.ValidClientID(cid) {
			fmt.Println("spotify setup failed: a client ID is 32 characters, 0-9 and a-f")
		} else if err := connectSpotifyTerminal(cid); err != nil {
			fmt.Println("spotify setup failed:", err)
		} else {
			cfg.Spotify.Mode = "api"
			cfg.Spotify.ClientID = cid
			// An older setup stored a client secret that nothing reads any more.
			_ = credman.Delete("spotify-secret")
			fmt.Println("spotify: connected (refresh token stored in Credential Manager)")
		}
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		fmt.Println("save config:", err)
		return 1
	}
	fmt.Printf("saved %s\n", cfgPath)
	fmt.Println("Start the daemon with: Yozora.exe serve")
	return 0
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// openURL hands an https URL to the system browser; the panel's WebView never opens it.
func openURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return errors.New("only https URLs open in the browser")
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", raw).Start()
}

func openBrowser(raw string) {
	if _, err := url.Parse(raw); err != nil {
		return
	}

	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", raw).Start(); err != nil {
		fmt.Println("open browser failed:", err, "— visit:", raw)
	}
}

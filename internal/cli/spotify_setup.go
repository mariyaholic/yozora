//go:build windows

package cli

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"uika-resonance/internal/spotifyapi"
)

// The terminal flow needs a fixed redirect URI because Spotify only accepts addresses registered
// on the developer app; the panel flow uses its own port instead.
const (
	setupSpotifyPort = 8977
	setupSpotifyPath = "/spotify/callback"
	setupSpotifyWait = spotifyapi.PKCEFlowTTL
)

// connectSpotifyTerminal links Spotify with the same PKCE flow the panel uses.
func connectSpotifyTerminal(clientID string) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", setupSpotifyPort))
	if err != nil {
		return fmt.Errorf("port %d is busy: %w", setupSpotifyPort, err)
	}
	return runSpotifyLink(&spotifyapi.Link{}, clientID, ln, func(u string) {
		fmt.Println("Opening browser for Spotify authorization. If it does not open, visit:")
		fmt.Println(u)
		openBrowser(u)
	}, setupSpotifyWait)
}

// runSpotifyLink begins a flow, hands the authorize URL to open, and waits on ln for the callback.
// It closes ln. The refresh token goes to link.Store and never passes through here.
func runSpotifyLink(link *spotifyapi.Link, clientID string, ln net.Listener, open func(string), wait time.Duration) error {
	defer ln.Close()
	authURL, err := link.Begin(clientID, "http://"+ln.Addr().String()+setupSpotifyPath)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(setupSpotifyPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		_, err := link.Complete(q.Get("state"), q.Get("code"), q.Get("error"))
		if errors.Is(err, spotifyapi.ErrStateMismatch) || errors.Is(err, spotifyapi.ErrNoPendingFlow) {
			// A stray request must not end the wait for the real callback.
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err != nil {
			fmt.Fprintln(w, "Yozora: Spotify was not connected. See the terminal.")
		} else {
			fmt.Fprintln(w, "Yozora: Spotify connected. You can close this tab.")
		}
		select {
		case done <- err:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer srv.Close()
	go func() { _ = srv.Serve(ln) }()
	open(authURL)
	select {
	case err := <-done:
		return err
	case <-time.After(wait):
		link.Cancel()
		return errors.New("timed out waiting for authorization")
	}
}

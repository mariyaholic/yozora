//go:build windows

package spotifyapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AuthorizeURL builds the user-consent URL for a BYO Spotify app.
func AuthorizeURL(clientID, redirectPort string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI(redirectPort))
	q.Set("scope", "user-read-currently-playing user-read-playback-state")
	return "https://accounts.spotify.com/authorize?" + q.Encode()
}

// redirectURI is the loopback callback used by both AuthorizeURL and
// Exchange; Spotify requires the two to match exactly.
func redirectURI(port string) string { return "http://127.0.0.1:" + port + "/callback" }

// Exchange exchanges the authorization code for tokens and stores the
// refresh token in the Credential Manager.
func (p *Provider) Exchange(code, redirectPort string) error {
	q := url.Values{}
	q.Set("grant_type", "authorization_code")
	q.Set("code", code)
	q.Set("redirect_uri", redirectURI(redirectPort))
	if p.ClientID != "" {
		q.Set("client_id", p.ClientID)
	}
	req, _ := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(q.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.Secret != "" {
		req.SetBasicAuth(p.ClientID, p.Secret)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("spotify: exchange: status %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return err
	}
	if tr.RefreshToken == "" {
		return errors.New("spotify: no refresh token in exchange response")
	}
	return StoreRefreshToken(tr.RefreshToken)
}

// WaitForCode runs a one-shot local callback server and returns the code.
func WaitForCode(port string) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return "", err
	}
	defer ln.Close()
	done := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	mux := http.NewServeMux()
	srv.Handler = mux
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if err := r.URL.Query().Get("error"); err != "" {
			fmt.Fprintln(w, "Authorization failed:", err)
			errCh <- errors.New("spotify: user denied: " + err)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "<html><body><h2>Yozora</h2><p>Spotify connected — you can close this tab.</p></body></html>")
		done <- code
	})
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	select {
	case code := <-done:
		return code, nil
	case err := <-errCh:
		return "", err
	case <-time.After(5 * time.Minute):
		return "", errors.New("spotify: timed out waiting for authorization")
	}
}

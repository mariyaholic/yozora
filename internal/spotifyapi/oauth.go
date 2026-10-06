//go:build windows

package spotifyapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func NewOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("spotify: generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func AuthorizeURL(clientID, redirectPort, state string) string {
	redirect := redirectURI(redirectPort)
	if redirect == "" || state == "" {
		return ""
	}
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirect)
	q.Set("scope", "user-read-currently-playing user-read-playback-state")
	q.Set("state", state)
	return "https://accounts.spotify.com/authorize?" + q.Encode()
}

func redirectURI(port string) string {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(n) + "/callback"
}

func (p *Provider) Exchange(code, redirectPort string) error {
	redirect := redirectURI(redirectPort)
	if redirect == "" {
		return errors.New("spotify: invalid redirect port")
	}
	q := url.Values{}
	q.Set("grant_type", "authorization_code")
	q.Set("code", code)
	q.Set("redirect_uri", redirect)
	if p.ClientID != "" {
		q.Set("client_id", p.ClientID)
	}
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(q.Encode()))
	if err != nil {
		return err
	}
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

func oauthCallbackHandler(state string, done chan<- string, errCh chan<- error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		if e := q.Get("error"); e != "" {
			fmt.Fprintln(w, "Authorization failed:", e)
			select {
			case errCh <- errors.New("spotify: user denied: " + e):
			default:
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "<html><body><h2>Yozora</h2><p>Spotify connected — you can close this tab.</p></body></html>")
		select {
		case done <- code:
		default:
		}
	})
}

func WaitForCode(port, state string) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return "", err
	}
	defer ln.Close()
	done := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	srv.Handler = http.NewServeMux()
	mux := srv.Handler.(*http.ServeMux)
	mux.Handle("/callback", oauthCallbackHandler(state, done, errCh))
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

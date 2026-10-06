//go:build windows

package spotifyapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"uika-resonance/internal/credman"
	"uika-resonance/internal/player"
)

const (
	tokenURL      = "https://accounts.spotify.com/api/token"
	nowPlayingURL = "https://api.spotify.com/v1/me/player"
	credKey       = "spotify-refresh"
)

type Provider struct {
	ClientID  string
	Secret    string
	Market    string
	PollEvery time.Duration

	Out chan player.Track

	http      *http.Client
	access    string
	accessExp time.Time
	lastErr   string
}

func New(clientID string, poll time.Duration) *Provider {
	return &Provider{
		ClientID:  clientID,
		PollEvery: poll,
		Out:       make(chan player.Track, 1),
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

func HasRefreshToken() bool {
	_, err := credman.Get(credKey)
	return err == nil
}

func StoreRefreshToken(tok string) error { return credman.Set(credKey, tok) }

func ClearRefreshToken() error { return credman.Delete(credKey) }

type tokenResp struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (p *Provider) Token() (string, error) {
	if p.access != "" && time.Now().Before(p.accessExp) {
		return p.access, nil
	}
	refresh, err := credman.Get(credKey)
	if err != nil || refresh == "" {
		return "", errors.New("spotify: no refresh token stored (run setup)")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	if p.ClientID != "" {
		form.Set("client_id", p.ClientID)
	}
	req, _ := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.Secret != "" {
		req.SetBasicAuth(p.ClientID, p.Secret)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("spotify: token refresh: status %d", resp.StatusCode)
	}
	var tr tokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	p.access = tr.AccessToken
	p.accessExp = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second / 2)
	return p.access, nil
}

type npResp struct {
	IsPlaying  bool  `json:"is_playing"`
	ProgressMS int64 `json:"progress_ms"`
	Item       *struct {
		Name    string `json:"name"`
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			Name   string `json:"name"`
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"album"`
		DurationMS   int64             `json:"duration_ms"`
		ExternalUrls map[string]string `json:"external_urls"`
	} `json:"item"`
}

func (p *Provider) Poll() (*player.Track, error) {
	tok, err := p.Token()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if p.Market != "" && p.Market != "from_token" {
		q.Set("market", p.Market)
	}
	req, _ := http.NewRequest(http.MethodGet, nowPlayingURL+"?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:

	case http.StatusUnauthorized:
		p.access = ""
		return nil, errors.New("spotify: unauthorized")
	default:
		return nil, fmt.Errorf("spotify: now-playing: status %d", resp.StatusCode)
	}
	var np npResp
	if err := json.NewDecoder(resp.Body).Decode(&np); err != nil {
		return nil, err
	}
	if np.Item == nil || strings.TrimSpace(np.Item.Name) == "" {
		return nil, nil
	}
	t := &player.Track{
		Source:      "spotifyapi",
		AppID:       "api.spotify.com",
		Player:      "Spotify",
		Title:       np.Item.Name,
		Album:       np.Item.Album.Name,
		Playing:     np.IsPlaying,
		PositionSec: float64(np.ProgressMS) / 1000,
		DurationSec: float64(np.Item.DurationMS) / 1000,
		LastUpdated: time.Now(),
		ArtURL:      firstImage(np.Item.Album.Images),
	}
	if len(np.Item.Artists) > 0 {
		t.Artist = np.Item.Artists[0].Name
	}
	if u, ok := np.Item.ExternalUrls["spotify"]; ok {
		t.ListenURL = u
	}
	return t, nil
}

func firstImage(imgs []struct {
	URL string `json:"url"`
}) string {
	best := ""
	for _, im := range imgs {
		if best == "" {
			best = im.URL
		}
	}
	return best
}

func (p *Provider) RunLoop(stop <-chan struct{}) {
	last := time.Time{}
	for {
		select {
		case <-stop:
			return
		case <-time.After(p.PollEvery):
		}
		t, err := p.Poll()
		if err != nil {
			if !strings.Contains(err.Error(), "no refresh token") {
				log.Printf("spotify: %v", err)
			}
			p.lastErr = err.Error()
			continue
		}
		p.lastErr = ""
		if t == nil {

			if last.IsZero() || time.Since(last) > 60*time.Second {
				p.Out <- player.Track{Source: "spotifyapi", Player: "Spotify", Title: " ", LastUpdated: time.Now()}
				last = time.Now()
			}
			continue
		}
		last = time.Now()
		select {
		case p.Out <- *t:
		default:
		}
	}
}

func (p *Provider) Err() string { return p.lastErr }

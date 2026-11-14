//go:build windows

package spotifyapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PKCEFlowTTL bounds how long a started authorization stays redeemable.
const PKCEFlowTTL = 10 * time.Minute

var clientIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidClientID reports whether id looks like a Spotify developer app client ID.
func ValidClientID(id string) bool { return clientIDPattern.MatchString(id) }

var (
	ErrNoPendingFlow  = errors.New("spotify: no authorization is waiting (start Connect again)")
	ErrFlowExpired    = errors.New("spotify: the authorization took too long (start Connect again)")
	ErrStateMismatch  = errors.New("spotify: authorization state does not match")
	ErrFlowSuperseded = errors.New("spotify: authorization was cancelled or replaced")
)

type pkceFlow struct {
	clientID, verifier, redirect, state string
	expires                             time.Time
}

// Link runs the panel's Authorization Code with PKCE flow. The verifier lives only in memory,
// a flow is redeemable once, and only the newest flow counts.
type Link struct {
	TokenURL string
	HTTP     *http.Client
	Now      func() time.Time
	Store    func(refresh string) error

	mu         sync.Mutex
	pending    *pkceFlow
	lastErr    string
	generation uint64
	flowState  string
}

// LinkStatus is what the panel shows about the connect flow itself.
type LinkStatus struct {
	Waiting bool   `json:"waiting"`
	Error   string `json:"error,omitempty"`
}

func (l *Link) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func randomURLToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Begin starts a flow for clientID and returns Spotify's authorize URL. Any earlier flow is dropped.
func (l *Link) Begin(clientID, redirect string) (string, error) {
	if !ValidClientID(clientID) {
		return "", errors.New("spotify: client ID must be 32 hexadecimal characters")
	}
	if u, err := url.Parse(redirect); err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" {
		return "", errors.New("spotify: redirect must be a 127.0.0.1 loopback URL")
	}
	// 64 random bytes encode to 86 characters, inside RFC 7636's 43-128.
	verifier, err := randomURLToken(64)
	if err != nil {
		return "", fmt.Errorf("spotify: generate verifier: %w", err)
	}
	state, err := randomURLToken(32)
	if err != nil {
		return "", fmt.Errorf("spotify: generate state: %w", err)
	}
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirect)
	q.Set("scope", "user-read-currently-playing user-read-playback-state")
	q.Set("state", state)
	q.Set("code_challenge_method", "S256")
	q.Set("code_challenge", pkceChallenge(verifier))
	l.mu.Lock()
	l.generation++
	l.flowState = state
	l.pending = &pkceFlow{clientID: clientID, verifier: verifier, redirect: redirect, state: state, expires: l.now().Add(PKCEFlowTTL)}
	l.lastErr = ""
	l.mu.Unlock()
	return "https://accounts.spotify.com/authorize?" + q.Encode(), nil
}

// Cancel forgets a waiting or in-flight flow.
func (l *Link) Cancel() { _ = l.CancelWith(nil) }

// CancelWith orders a disconnect transaction against a completing authorization.
// The callback must not call back into Link.
func (l *Link) CancelWith(disconnect func() error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.generation++
	l.pending = nil
	l.flowState = ""
	l.lastErr = ""
	if disconnect != nil {
		return disconnect()
	}
	return nil
}

// CancelState cancels only the flow associated with state, including an
// exchange already in flight. A browser launch failure from an older request
// must not cancel a newer authorization.
func (l *Link) CancelState(state string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(l.flowState)) != 1 {
		return
	}
	l.generation++
	l.pending = nil
	l.flowState = ""
	l.lastErr = ""
}

// Status reports whether a flow is waiting and the last flow error. An expired flow is dropped here.
func (l *Link) Status() LinkStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending != nil && !l.now().Before(l.pending.expires) {
		l.pending = nil
		l.flowState = ""
		l.lastErr = ErrFlowExpired.Error()
	}
	return LinkStatus{Waiting: l.pending != nil, Error: l.lastErr}
}

// Complete redeems a callback and stores its refresh token.
func (l *Link) Complete(state, code, denied string) (string, error) {
	return l.CompleteWith(state, code, denied, nil)
}

// CompleteWith commits the token and callback together with respect to Begin and
// CancelWith. The exchange runs outside the mutex; a newer flow or cancellation
// invalidates its result. commit must not call back into Link.
func (l *Link) CompleteWith(state, code, denied string, commit func(string) error) (string, error) {
	return l.CompleteWithPhases(state, code, denied, nil, commit)
}

// CompleteWithPhases runs beforeStore after validating the flow and before
// replacing its credential. This lets callers stop an old token-refreshing
// poller before installing the newly authorized token. Both callbacks run under
// the Link mutex and must not call back into Link.
func (l *Link) CompleteWithPhases(state, code, denied string, beforeStore func() error, commit func(string) error) (string, error) {
	l.mu.Lock()
	flow := l.pending
	if flow == nil {
		l.mu.Unlock()
		return "", ErrNoPendingFlow
	}
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(flow.state)) != 1 {
		l.mu.Unlock()
		return "", ErrStateMismatch
	}
	l.pending = nil
	generation := l.generation
	l.mu.Unlock()

	refresh, err := l.redeem(flow, code, denied)
	l.mu.Lock()
	defer l.mu.Unlock()
	if generation != l.generation {
		return "", ErrFlowSuperseded
	}
	l.flowState = ""
	if err == nil && beforeStore != nil {
		err = beforeStore()
	}
	if err == nil {
		store := l.Store
		if store == nil {
			store = StoreRefreshToken
		}
		if err = store(refresh); err != nil {
			err = fmt.Errorf("spotify: store token: %w", err)
		} else if commit != nil {
			err = commit(flow.clientID)
		}
	}
	if err != nil {
		l.lastErr = err.Error()
		return "", err
	}
	l.lastErr = ""
	return flow.clientID, nil
}

func (l *Link) redeem(flow *pkceFlow, code, denied string) (string, error) {
	if !l.now().Before(flow.expires) {
		return "", ErrFlowExpired
	}
	if denied != "" {
		// Spotify's error values are short ASCII codes such as access_denied; keep only those.
		if len(denied) > 64 || strings.Trim(denied, "abcdefghijklmnopqrstuvwxyz_") != "" {
			denied = "unknown"
		}
		return "", errors.New("spotify: authorization was not granted (" + denied + ")")
	}
	if code == "" {
		return "", errors.New("spotify: callback carried no authorization code")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", flow.redirect)
	form.Set("client_id", flow.clientID)
	form.Set("code_verifier", flow.verifier)
	endpoint := l.TokenURL
	if endpoint == "" {
		endpoint = tokenURL
	}
	client := l.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		// The transport error names the URL only, never the form body.
		return "", fmt.Errorf("spotify: token exchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("spotify: token exchange: status %d", resp.StatusCode)
	}
	var tr struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&tr); err != nil {
		return "", errors.New("spotify: token exchange: unreadable response")
	}
	if tr.RefreshToken == "" {
		return "", errors.New("spotify: no refresh token in exchange response")
	}
	return tr.RefreshToken, nil
}

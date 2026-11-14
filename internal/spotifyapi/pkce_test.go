//go:build windows

package spotifyapi

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"uika-resonance/internal/player"
)

const testClientID = "0123456789abcdef0123456789abcdef"
const testRedirect = "http://127.0.0.1:50123/spotify/callback"

type fakeTokenEndpoint struct {
	mu    sync.Mutex
	forms []url.Values
	reply string
	code  int
}

func (f *fakeTokenEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.forms = append(f.forms, r.PostForm)
	reply, code := f.reply, f.code
	f.mu.Unlock()
	if _, _, basic := r.BasicAuth(); basic {
		http.Error(w, "a PKCE client must not send a secret", http.StatusBadRequest)
		return
	}
	if code == 0 {
		code = http.StatusOK
	}
	w.WriteHeader(code)
	_, _ = w.Write([]byte(reply))
}

func newTestLink(t *testing.T, endpoint *fakeTokenEndpoint) (*Link, *[]string, *time.Time) {
	t.Helper()
	srv := httptest.NewServer(endpoint)
	t.Cleanup(srv.Close)
	stored := []string{}
	now := time.Unix(1_760_000_000, 0)
	link := &Link{
		TokenURL: srv.URL,
		HTTP:     srv.Client(),
		Now:      func() time.Time { return now },
		Store:    func(tok string) error { stored = append(stored, tok); return nil },
	}
	return link, &stored, &now
}

func authorizeQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "accounts.spotify.com" || u.Path != "/authorize" {
		t.Fatalf("authorize URL = %s", raw)
	}
	return u.Query()
}

func TestPKCEFlowExchangesWithVerifierAndStoresToken(t *testing.T) {
	endpoint := &fakeTokenEndpoint{reply: `{"access_token":"a","refresh_token":"refresh-secret","expires_in":3600}`}
	link, stored, _ := newTestLink(t, endpoint)
	raw, err := link.Begin(testClientID, testRedirect)
	if err != nil {
		t.Fatal(err)
	}
	q := authorizeQuery(t, raw)
	if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != testClientID || q.Get("redirect_uri") != testRedirect || q.Get("response_type") != "code" {
		t.Fatalf("authorize query = %v", q)
	}
	if !link.Status().Waiting {
		t.Fatal("flow not waiting after Begin")
	}
	clientID, err := link.Complete(q.Get("state"), "the-code", "")
	if err != nil || clientID != testClientID {
		t.Fatalf("Complete = %q, %v", clientID, err)
	}
	if len(*stored) != 1 || (*stored)[0] != "refresh-secret" {
		t.Fatalf("stored = %v", *stored)
	}
	form := endpoint.forms[0]
	verifier := form.Get("code_verifier")
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Fatalf("verifier length %d", len(verifier))
	}
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") {
		t.Fatal("verifier does not match the S256 challenge")
	}
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "the-code" || form.Get("redirect_uri") != testRedirect || form.Get("client_id") != testClientID || form.Has("client_secret") {
		t.Fatalf("token form = %v", form)
	}
	if s := link.Status(); s.Waiting || s.Error != "" {
		t.Fatalf("status after success = %+v", s)
	}
}

func TestPKCECallbackIsSingleUse(t *testing.T) {
	endpoint := &fakeTokenEndpoint{reply: `{"refresh_token":"r"}`}
	link, stored, _ := newTestLink(t, endpoint)
	raw, _ := link.Begin(testClientID, testRedirect)
	state := authorizeQuery(t, raw).Get("state")
	if _, err := link.Complete(state, "code", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := link.Complete(state, "code", ""); !errors.Is(err, ErrNoPendingFlow) {
		t.Fatalf("replay err = %v", err)
	}
	if len(endpoint.forms) != 1 || len(*stored) != 1 {
		t.Fatalf("replay reached the token endpoint: %d exchanges, %d stores", len(endpoint.forms), len(*stored))
	}
}

func TestPKCEWrongStateLeavesRealFlowWaiting(t *testing.T) {
	endpoint := &fakeTokenEndpoint{reply: `{"refresh_token":"r"}`}
	link, _, _ := newTestLink(t, endpoint)
	raw, _ := link.Begin(testClientID, testRedirect)
	state := authorizeQuery(t, raw).Get("state")
	for _, forged := range []string{"", "forged", state + "x", strings.ToUpper(state)} {
		if _, err := link.Complete(forged, "code", ""); !errors.Is(err, ErrStateMismatch) {
			t.Fatalf("forged state %q err = %v", forged, err)
		}
	}
	if len(endpoint.forms) != 0 {
		t.Fatal("a forged state reached the token endpoint")
	}
	if _, err := link.Complete(state, "code", ""); err != nil {
		t.Fatalf("real callback after forgeries: %v", err)
	}
}

func TestPKCENewerFlowReplacesOlder(t *testing.T) {
	link, _, _ := newTestLink(t, &fakeTokenEndpoint{reply: `{"refresh_token":"r"}`})
	first, _ := link.Begin(testClientID, testRedirect)
	second, _ := link.Begin(testClientID, testRedirect)
	if _, err := link.Complete(authorizeQuery(t, first).Get("state"), "code", ""); !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("stale flow err = %v", err)
	}
	if _, err := link.Complete(authorizeQuery(t, second).Get("state"), "code", ""); err != nil {
		t.Fatal(err)
	}
}

func TestPKCEExpiryCancelAndDenial(t *testing.T) {
	endpoint := &fakeTokenEndpoint{reply: `{"refresh_token":"r"}`}
	link, stored, now := newTestLink(t, endpoint)
	raw, _ := link.Begin(testClientID, testRedirect)
	*now = now.Add(PKCEFlowTTL)
	if _, err := link.Complete(authorizeQuery(t, raw).Get("state"), "code", ""); !errors.Is(err, ErrFlowExpired) {
		t.Fatalf("expired err = %v", err)
	}
	raw, _ = link.Begin(testClientID, testRedirect)
	*now = now.Add(PKCEFlowTTL)
	if s := link.Status(); s.Waiting || s.Error == "" {
		t.Fatalf("status after expiry = %+v", s)
	}
	if _, err := link.Complete(authorizeQuery(t, raw).Get("state"), "code", ""); !errors.Is(err, ErrNoPendingFlow) {
		t.Fatalf("callback after expiry was noticed = %v", err)
	}

	raw, _ = link.Begin(testClientID, testRedirect)
	link.Cancel()
	if _, err := link.Complete(authorizeQuery(t, raw).Get("state"), "code", ""); !errors.Is(err, ErrNoPendingFlow) {
		t.Fatalf("cancelled flow err = %v", err)
	}

	raw, _ = link.Begin(testClientID, testRedirect)
	_, err := link.Complete(authorizeQuery(t, raw).Get("state"), "", "access_denied")
	if err == nil || !strings.Contains(err.Error(), "access_denied") || !strings.Contains(link.Status().Error, "not granted") {
		t.Fatalf("denial err = %v status = %+v", err, link.Status())
	}
	raw, _ = link.Begin(testClientID, testRedirect)
	_, err = link.Complete(authorizeQuery(t, raw).Get("state"), "", "<script>")
	if err == nil || strings.Contains(err.Error(), "<") {
		t.Fatalf("hostile error parameter echoed: %v", err)
	}
	if len(endpoint.forms) != 0 || len(*stored) != 0 {
		t.Fatal("an expired, cancelled or denied flow exchanged a code")
	}
}

func TestPKCEExchangeFailuresStoreNothingAndHideTokens(t *testing.T) {
	for _, tc := range []struct {
		name  string
		code  int
		reply string
	}{
		{"rejected", http.StatusBadRequest, `{"error":"invalid_grant","refresh_token":"leak"}`},
		{"no refresh", http.StatusOK, `{"access_token":"access-secret"}`},
		{"garbage", http.StatusOK, `not json access-secret`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link, stored, _ := newTestLink(t, &fakeTokenEndpoint{code: tc.code, reply: tc.reply})
			raw, _ := link.Begin(testClientID, testRedirect)
			_, err := link.Complete(authorizeQuery(t, raw).Get("state"), "the-code", "")
			if err == nil || len(*stored) != 0 {
				t.Fatalf("err = %v, stored = %v", err, *stored)
			}
			for _, secret := range []string{"leak", "access-secret", "the-code"} {
				if strings.Contains(err.Error(), secret) || strings.Contains(link.Status().Error, secret) {
					t.Fatalf("error exposes %q: %v", secret, err)
				}
			}
		})
	}
}

func TestPKCEBeginValidatesInput(t *testing.T) {
	link := &Link{}
	for _, id := range []string{"", "short", strings.Repeat("g", 32), strings.ToUpper(testClientID), testClientID + "0"} {
		if _, err := link.Begin(id, testRedirect); err == nil {
			t.Errorf("accepted client ID %q", id)
		}
	}
	for _, redirect := range []string{"", "https://127.0.0.1:1/cb", "http://localhost:1/cb", "http://127.0.0.1/cb", "http://evil.test:1/cb"} {
		if _, err := link.Begin(testClientID, redirect); err == nil {
			t.Errorf("accepted redirect %q", redirect)
		}
	}
	if link.Status().Waiting {
		t.Fatal("a rejected Begin left a flow waiting")
	}
}

func TestManagerStartStopReplacesPollerAndWithdrawsTrack(t *testing.T) {
	m := NewManager()
	started := make(chan *Provider, 4)
	m.newLoop = func(p *Provider, stop <-chan struct{}) {
		started <- p
		<-stop
	}
	if m.Running() {
		t.Fatal("new manager is running")
	}
	m.Start(testClientID, "from_token", time.Minute)
	first := <-started
	if !m.Running() || first.ClientID != testClientID || first.Out != m.out {
		t.Fatal("poller not started on the shared channel")
	}
	m.out <- player.Track{Source: "spotifyapi", Title: "Song"}
	m.Start("ffffffffffffffffffffffffffffffff", "SE", time.Minute)
	second := <-started
	if second.ClientID != "ffffffffffffffffffffffffffffffff" || second.Market != "SE" {
		t.Fatalf("replacement poller = %+v", second)
	}
	if got := <-m.Out(); got.Title != "" {
		t.Fatalf("restart left the old track queued: %+v", got)
	}
	m.Stop()
	if m.Running() {
		t.Fatal("still running after Stop")
	}
	if got := <-m.Out(); got.Source != "spotifyapi" || got.Title != "" {
		t.Fatalf("Stop did not withdraw the track: %+v", got)
	}
	m.Stop() // idempotent
}

func TestPKCEInFlightExchangeIsSuperseded(t *testing.T) {
	for _, action := range []string{"cancel", "cancel state", "begin"} {
		t.Run(action, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				_, _ = w.Write([]byte(`{"refresh_token":"old"}`))
			}))
			defer srv.Close()
			stored, committed := false, false
			link := &Link{TokenURL: srv.URL, HTTP: srv.Client(), Store: func(string) error { stored = true; return nil }}
			raw, err := link.Begin(testClientID, testRedirect)
			if err != nil {
				t.Fatal(err)
			}
			state := authorizeQuery(t, raw).Get("state")
			done := make(chan error, 1)
			go func() {
				_, err := link.CompleteWith(state, "code", "", func(string) error { committed = true; return nil })
				done <- err
			}()
			<-entered
			if action == "cancel" {
				link.Cancel()
			} else if action == "cancel state" {
				link.CancelState(state)
			} else if _, err := link.Begin(testClientID, testRedirect); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; !errors.Is(err, ErrFlowSuperseded) {
				t.Fatalf("completion error = %v", err)
			}
			if stored || committed {
				t.Fatalf("stale result applied: stored=%v committed=%v", stored, committed)
			}
			status := link.Status()
			if status.Error != "" || status.Waiting != (action == "begin") {
				t.Fatalf("status = %+v", status)
			}
		})
	}
}

func TestPKCECommitAndDisconnectAreOrdered(t *testing.T) {
	link, _, _ := newTestLink(t, &fakeTokenEndpoint{reply: `{"refresh_token":"r"}`})
	raw, _ := link.Begin(testClientID, testRedirect)
	state := authorizeQuery(t, raw).Get("state")
	entered, release, completed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := link.CompleteWith(state, "code", "", func(string) error {
			close(entered)
			<-release
			return nil
		})
		completed <- err
	}()
	<-entered
	cancelled := make(chan struct{})
	go func() { _ = link.CancelWith(func() error { close(cancelled); return nil }) }()
	select {
	case <-cancelled:
		t.Fatal("disconnect ran during authorization commit")
	default:
	}
	close(release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	<-cancelled
}

func TestPKCECancelStateLeavesNewerFlow(t *testing.T) {
	link, _, _ := newTestLink(t, &fakeTokenEndpoint{reply: `{"refresh_token":"r"}`})
	first, _ := link.Begin(testClientID, testRedirect)
	second, _ := link.Begin(testClientID, testRedirect)
	link.CancelState(authorizeQuery(t, first).Get("state"))
	if !link.Status().Waiting {
		t.Fatal("old cancellation discarded the new flow")
	}
	if _, err := link.Complete(authorizeQuery(t, second).Get("state"), "code", ""); err != nil {
		t.Fatal(err)
	}
}

func TestPKCEStopsOldRefreshBeforeStoringNewToken(t *testing.T) {
	endpoint := &fakeTokenEndpoint{reply: `{"refresh_token":"new-token"}`}
	link, _, _ := newTestLink(t, endpoint)
	credential := "original"
	link.Store = func(token string) error { credential = token; return nil }
	raw, _ := link.Begin(testClientID, testRedirect)
	_, err := link.CompleteWithPhases(authorizeQuery(t, raw).Get("state"), "code", "", func() error {
		// Stopping waits for an old poller whose final refresh can rotate its token.
		credential = "old-rotated-token"
		return nil
	}, func(string) error {
		if credential != "new-token" {
			t.Errorf("commit sees credential %q", credential)
		}
		return nil
	})
	if err != nil || credential != "new-token" {
		t.Fatalf("credential = %q, error = %v", credential, err)
	}
}

func TestPKCEPreStoreFailureLeavesTokenUnchanged(t *testing.T) {
	link, stored, _ := newTestLink(t, &fakeTokenEndpoint{reply: `{"refresh_token":"new"}`})
	raw, _ := link.Begin(testClientID, testRedirect)
	stopErr := errors.New("stop failed")
	committed := false
	_, err := link.CompleteWithPhases(authorizeQuery(t, raw).Get("state"), "code", "", func() error { return stopErr }, func(string) error { committed = true; return nil })
	if !errors.Is(err, stopErr) || len(*stored) != 0 || committed {
		t.Fatalf("err=%v, stored=%v, committed=%v", err, *stored, committed)
	}
}

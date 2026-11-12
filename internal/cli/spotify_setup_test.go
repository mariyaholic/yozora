//go:build windows

package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"uika-resonance/internal/spotifyapi"
)

const fixtureClientID = "0123456789abcdef0123456789abcdef"

func fakeTokenServer(t *testing.T, status int, gotForm *url.Values) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		*gotForm = r.PostForm
		w.WriteHeader(status)
		w.Write([]byte(`{"access_token":"a","refresh_token":"fixture-refresh"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// browser plays the user: it follows the authorize URL's redirect_uri back with a code.
func browser(t *testing.T, extra string) func(string) {
	return func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Error(err)
			return
		}
		q := u.Query()
		go func() {
			cb := q.Get("redirect_uri") + "?state=" + url.QueryEscape(q.Get("state")) + extra
			// A forged callback first: it must not end the wait.
			if resp, err := http.Get(q.Get("redirect_uri") + "?state=forged&code=evil"); err == nil {
				resp.Body.Close()
			}
			if resp, err := http.Get(cb); err == nil {
				resp.Body.Close()
			}
		}()
	}
}

func TestSetupLinksSpotifyWithPKCE(t *testing.T) {
	var form url.Values
	tok := fakeTokenServer(t, 200, &form)
	var stored string
	link := &spotifyapi.Link{TokenURL: tok.URL, Store: func(r string) error { stored = r; return nil }}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := runSpotifyLink(link, fixtureClientID, ln, browser(t, "&code=fixture-code"), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if stored != "fixture-refresh" {
		t.Fatalf("stored %q", stored)
	}
	if form.Get("code") != "fixture-code" || form.Get("client_id") != fixtureClientID || form.Get("code_verifier") == "" || form.Get("client_secret") != "" {
		t.Fatalf("token request %v", form)
	}
	if !strings.HasPrefix(form.Get("redirect_uri"), "http://127.0.0.1:") || !strings.HasSuffix(form.Get("redirect_uri"), setupSpotifyPath) {
		t.Fatalf("redirect %q", form.Get("redirect_uri"))
	}
}

func TestSetupReportsDeniedAndFailedExchange(t *testing.T) {
	var form url.Values
	tok := fakeTokenServer(t, 400, &form)
	for name, extra := range map[string]string{"denied": "&error=access_denied", "exchange fails": "&code=c"} {
		link := &spotifyapi.Link{TokenURL: tok.URL, Store: func(string) error { t.Error("token stored"); return nil }}
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		if err := runSpotifyLink(link, fixtureClientID, ln, browser(t, extra), 5*time.Second); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSetupTimesOutAndRejectsBadClientID(t *testing.T) {
	link := &spotifyapi.Link{}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	if err := runSpotifyLink(link, fixtureClientID, ln, func(string) {}, 50*time.Millisecond); err == nil || link.Status().Waiting {
		t.Fatalf("timeout: err=%v waiting=%v", err, link.Status().Waiting)
	}
	ln, _ = net.Listen("tcp", "127.0.0.1:0")
	if err := runSpotifyLink(&spotifyapi.Link{}, "short", ln, func(string) { t.Error("opened") }, time.Second); err == nil {
		t.Fatal("bad client ID accepted")
	}
}

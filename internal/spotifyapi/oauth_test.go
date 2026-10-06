//go:build windows

package spotifyapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuthCallbackRejectsWrongState(t *testing.T) {
	done := make(chan string, 1)
	errors := make(chan error, 1)
	h := oauthCallbackHandler("expected-state", done, errors)

	bad := httptest.NewRecorder()
	h.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/callback?code=forged&state=wrong", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("wrong state status=%d", bad.Code)
	}
	select {
	case code := <-done:
		t.Fatalf("wrong state delivered authorization code %q", code)
	default:
	}

	good := httptest.NewRecorder()
	h.ServeHTTP(good, httptest.NewRequest(http.MethodGet, "/callback?code=valid-code&state=expected-state", nil))
	if good.Code != http.StatusOK {
		t.Fatalf("valid state status=%d", good.Code)
	}
	if code := <-done; code != "valid-code" {
		t.Fatalf("code=%q", code)
	}
}

func TestAuthorizeURLEncodesClientIDAndIncludesState(t *testing.T) {
	u, err := url.Parse(AuthorizeURL("client&id=other", "8977", "random-state"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "client&id=other" || q.Get("state") != "random-state" {
		t.Fatalf("authorize query=%v", q)
	}
}

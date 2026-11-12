//go:build windows

package presence

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/discordipc"
	"uika-resonance/internal/player"
)

type echoActivityClient struct {
	recordingActivityClient
	echo  json.RawMessage
	alive bool
}

func (c *echoActivityClient) AcceptedPayload() json.RawMessage { return c.echo }
func (c *echoActivityClient) Alive() bool                      { return c.alive }
func (c *echoActivityClient) Close()                           { c.alive = false }

func newTestCardEngine(t *testing.T, client *echoActivityClient, tracks ...player.Track) *Engine {
	t.Helper()
	holder, err := config.NewHolder(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Cfg: holder, lastSMTCSessions: tracks}
	e.dial = func(string) (activityClient, error) {
		client.alive = true
		return client, nil
	}
	return e
}

func isTestCard(a *discordipc.Activity) bool { return a != nil && a.Details == "Yozora test card" }

func TestTestCardWithNothingPlayingIsClearedWhenItExpires(t *testing.T) {
	client := &echoActivityClient{echo: json.RawMessage(`{"details":"Yozora test card"}`)}
	e := newTestCardEngine(t, client)
	now := time.Unix(1_760_000_000, 0)
	if err := e.SendTestCard(); err != nil {
		t.Fatal(err)
	}
	if err := e.SendTestCard(); !errors.Is(err, ErrTestCardBusy) {
		t.Fatalf("second request err = %v", err)
	}
	e.step(now)
	if len(client.activities) != 1 || !isTestCard(client.activities[0]) {
		t.Fatalf("test card not published: %+v", client.activities)
	}
	card := e.Status().TestCard
	if card == nil || card.State != "shown" || !card.Confirmed || !card.Until.Equal(now.Add(TestCardFor)) {
		t.Fatalf("status = %+v", card)
	}
	if !e.Status().Connected {
		t.Fatal("a sent test card should leave Discord connected")
	}
	if err := e.SendTestCard(); !errors.Is(err, ErrTestCardBusy) {
		t.Fatalf("request while shown err = %v", err)
	}
	for at := time.Duration(0); at < TestCardFor; at += time.Second {
		e.step(now.Add(at))
	}
	if len(client.activities) != 1 {
		t.Fatalf("activity changed while the test card was up: %+v", client.activities[1:])
	}
	e.step(now.Add(TestCardFor))
	if len(client.activities) != 2 || client.activities[1] != nil {
		t.Fatalf("expired test card was not cleared: %+v", client.activities)
	}
	if card := e.Status().TestCard; card.State != "done" {
		t.Fatalf("state after expiry = %+v", card)
	}
	for at := TestCardFor; at < 3*TestCardFor; at += time.Second {
		e.step(now.Add(at))
	}
	if len(client.activities) != 2 {
		t.Fatalf("idle engine published after the test: %+v", client.activities[2:])
	}
	if err := e.SendTestCard(); err != nil {
		t.Fatalf("a finished test should allow another: %v", err)
	}
}

func TestTestCardHandsBackToThePlayingTrack(t *testing.T) {
	client := &echoActivityClient{}
	start := time.Unix(1_760_000_000, 0)
	track := player.Track{Source: "spotify", Player: "Spotify", Title: "Real Song", Artist: "Artist", Playing: true, LastUpdated: start, DurationSec: 600}
	e := newTestCardEngine(t, client, track)
	e.ipc, client.alive = client, true
	e.step(start)
	e.step(start.Add(time.Second))
	if len(client.activities) != 1 || client.activities[0].Details != "Real Song" {
		t.Fatalf("track not published first: %+v", client.activities)
	}
	if err := e.SendTestCard(); err != nil {
		t.Fatal(err)
	}
	now := start.Add(2 * time.Second)
	e.step(now)
	if !isTestCard(client.activities[len(client.activities)-1]) {
		t.Fatal("test card did not replace the track")
	}
	if card := e.Status().TestCard; card.Confirmed {
		t.Fatal("an empty readback must not count as confirmed")
	}
	if e.Status().Published == nil || !isTestCard(e.Status().Published) {
		t.Fatal("status should show the test card while it is up")
	}
	for at := time.Duration(0); at <= TestCardFor+10*time.Second; at += 250 * time.Millisecond {
		e.step(now.Add(at))
	}
	last := client.activities[len(client.activities)-1]
	if last == nil || last.Details != "Real Song" {
		t.Fatalf("track not restored after the test: %+v", last)
	}
	if card := e.Status().TestCard; card.State != "done" {
		t.Fatalf("state = %+v", card)
	}
}

// A browser track waiting for classification never publishes, so the expiry must not rely on a send.
func TestTestCardCannotStickBehindAPendingTrack(t *testing.T) {
	client := &echoActivityClient{}
	now := time.Unix(1_760_000_000, 0)
	e := newTestCardEngine(t, client)
	if err := e.SendTestCard(); err != nil {
		t.Fatal(err)
	}
	e.step(now)
	e.mu.Lock()
	e.lastSMTCSessions = []player.Track{{Source: "browser", Player: "Edge", Title: "Some video", Playing: true, LastUpdated: now}}
	e.classifyPending = true
	e.mu.Unlock()
	e.step(now.Add(TestCardFor))
	for at := TestCardFor; at < 2*TestCardFor; at += time.Second {
		e.step(now.Add(at))
	}
	sawClear := false
	for _, a := range client.activities[1:] {
		if a == nil {
			sawClear = true
		}
		if isTestCard(a) {
			t.Fatal("test card republished")
		}
	}
	if !sawClear {
		t.Fatalf("test card left standing: %+v", client.activities)
	}
}

func TestTestCardFailuresDoNotHoldTheEngine(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	holder, err := config.NewHolder(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Cfg: holder, dial: func(string) (activityClient, error) { return nil, errors.New("discordipc: no discord pipe found") }}
	e.nextDial.Store(now.Add(time.Hour).UnixNano())
	if err := e.SendTestCard(); err != nil {
		t.Fatal(err)
	}
	e.step(now)
	card := e.Status().TestCard
	if card.State != "failed" || card.Error == "" {
		t.Fatalf("dial failure = %+v", card)
	}
	if err := e.SendTestCard(); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}

	client := &echoActivityClient{}
	client.err = errors.New("discordipc: SET_ACTIVITY timed out")
	e2 := newTestCardEngine(t, client)
	if err := e2.SendTestCard(); err != nil {
		t.Fatal(err)
	}
	if e2.stepTestCard(now) {
		t.Fatal("a failed send must not hold the engine")
	}
	if card := e2.Status().TestCard; card.State != "failed" || e2.Status().Connected {
		t.Fatalf("send failure = %+v connected=%v", card, e2.Status().Connected)
	}
}

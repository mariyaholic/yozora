//go:build windows

package presence

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/discordipc"
	"uika-resonance/internal/player"
	"uika-resonance/internal/smtc"
)

type reconnectClient struct {
	alive bool
	sent  []*discordipc.Activity
}

func (c *reconnectClient) Alive() bool                      { return c.alive }
func (c *reconnectClient) Close()                           { c.alive = false }
func (c *reconnectClient) Ping() error                      { return nil }
func (c *reconnectClient) AcceptedPayload() json.RawMessage { return nil }
func (c *reconnectClient) SetActivity(a *discordipc.Activity) error {
	c.sent = append(c.sent, a)
	return nil
}

func TestUnchangedTrackRepublishesAfterDiscordDisconnect(t *testing.T) {
	h, err := config.NewHolder(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	track := player.Track{Source: "spotify", Title: "song", Playing: true, LastUpdated: now}
	var clients []*reconnectClient
	e := &Engine{Cfg: h, lastSMTCSessions: []player.Track{track}, dial: func(string) (activityClient, error) {
		c := &reconnectClient{alive: true}
		clients = append(clients, c)
		return c, nil
	}}
	e.step(now)
	e.step(now.Add(time.Second))
	if len(clients) != 1 || len(clients[0].sent) != 1 {
		t.Fatalf("initial publish: %+v", clients)
	}
	clients[0].alive = false
	e.nextDial.Store(0)
	e.step(now.Add(7 * time.Second))
	if len(clients) != 2 || len(clients[1].sent) != 1 || clients[1].sent[0] == nil {
		t.Fatalf("unchanged track was not republished after reconnect: %+v", clients)
	}
}

func TestSourceClientOverrideChangesConnection(t *testing.T) {
	h, err := config.NewHolder(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	h.Get().Discord.Apps = map[string]string{"spotify": "client-a", "applemusic": "client-b"}
	now := time.Now()
	first := player.Track{Source: "spotify", Title: "one", Playing: true, LastUpdated: now}
	var ids []string
	var clients []*reconnectClient
	e := &Engine{Cfg: h, lastSMTCSessions: []player.Track{first}, dial: func(id string) (activityClient, error) {
		ids = append(ids, id)
		c := &reconnectClient{alive: true}
		clients = append(clients, c)
		return c, nil
	}}
	e.step(now)
	e.step(now.Add(time.Second))
	e.lastSMTCSessions = []player.Track{{Source: "applemusic", Title: "two", Playing: true, LastUpdated: now}}
	e.step(now.Add(7 * time.Second))
	e.step(now.Add(8 * time.Second))
	if len(ids) != 2 || ids[0] != "client-a" || ids[1] != "client-b" || clients[0].alive || len(clients[1].sent) != 1 {
		t.Fatalf("switch failed: ids=%v clients=%+v", ids, clients)
	}
}

func TestPresentationSettingsRefreshUnchangedTrackAfterRateGate(t *testing.T) {
	h, err := config.NewHolder(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	track := player.Track{Source: "spotify", Title: "song", Playing: true, LastUpdated: now}
	client := &reconnectClient{alive: true}
	e := &Engine{Cfg: h, ipc: client, lastSMTCSessions: []player.Track{track}}
	e.step(now)
	e.step(now.Add(time.Second))
	if len(client.sent) != 1 {
		t.Fatalf("initial frames: %d", len(client.sent))
	}
	h.Get().Buttons.Enabled = false
	e.step(now.Add(2 * time.Second))
	if len(client.sent) != 1 {
		t.Fatal("changed configuration bypassed the rate gate")
	}
	e.step(now.Add(7 * time.Second))
	if len(client.sent) != 2 || len(client.sent[1].Buttons) != 0 {
		t.Fatalf("configuration not refreshed: %+v", client.sent)
	}
}

func TestRunDisablesClosedInputChannels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		smtcCh := make(chan []smtc.Track)
		spotifyCh := make(chan player.Track)
		close(smtcCh)
		close(spotifyCh)
		e := &Engine{smtcCh: smtcCh, spotifyCh: spotifyCh, lastSMTCSessions: []player.Track{{Title: "stale"}}, spotify: &player.Track{Title: "stale"}}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { e.Run(ctx); close(done) }()
		synctest.Wait()
		if e.smtcCh != nil || e.spotifyCh != nil || e.spotify != nil || len(e.lastSMTCSessions) != 0 {
			t.Fatal("closed input remains selectable")
		}
		cancel()
		<-done
	})
}

func TestFailedClientBackoffDoesNotDelayDifferentApplication(t *testing.T) {
	now := time.Now()
	var ids []string
	e := &Engine{dial: func(id string) (activityClient, error) {
		ids = append(ids, id)
		if id == "client-a" {
			return nil, errors.New("offline")
		}
		return &reconnectClient{alive: true}, nil
	}}
	if e.ensureIPC("client-a", now) {
		t.Fatal("failed dial succeeded")
	}
	if e.ensureIPC("client-a", now.Add(time.Second)) {
		t.Fatal("retry bypassed backoff")
	}
	if !e.ensureIPC("client-b", now.Add(2*time.Second)) {
		t.Fatal("new application inherited failed application's backoff")
	}
	if len(ids) != 2 || ids[0] != "client-a" || ids[1] != "client-b" {
		t.Fatalf("dial sequence = %v", ids)
	}
}

func TestClearAfterDisconnectKeepsSourceApplication(t *testing.T) {
	h, err := config.NewHolder(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	client := &reconnectClient{alive: true}
	var id string
	e := &Engine{Cfg: h, ipcClientID: "source-client", dial: func(got string) (activityClient, error) { id = got; return client, nil }}
	e.disconnect()
	e.clear(now, "")
	if id != "source-client" || len(client.sent) != 1 || client.sent[0] != nil {
		t.Fatalf("clear used %q: %+v", id, client.sent)
	}
}

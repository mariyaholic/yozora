//go:build windows

package spotifyapi

import (
	"sync"
	"time"

	"uika-resonance/internal/player"
)

// Manager owns at most one Web API poller and a channel that outlives it, so the panel can
// connect or disconnect Spotify without restarting the daemon.
type Manager struct {
	out chan player.Track

	opMu     sync.Mutex // serialises Start and Stop
	mu       sync.Mutex
	provider *Provider
	stop     chan struct{}
	done     chan struct{}
	newLoop  func(*Provider, <-chan struct{})
}

func NewManager() *Manager {
	return &Manager{out: make(chan player.Track, 1)}
}

// Out carries the newest Web API snapshot from whichever poller is running.
func (m *Manager) Out() <-chan player.Track { return m.out }

// Start replaces any running poller with one for clientID.
func (m *Manager) Start(clientID, market string, every time.Duration) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked()
	p := New(clientID, every)
	p.Market = market
	p.Out = m.out
	stop, done := make(chan struct{}), make(chan struct{})
	run := m.newLoop
	if run == nil {
		run = func(p *Provider, stop <-chan struct{}) { p.RunLoop(stop) }
	}
	m.mu.Lock()
	m.provider, m.stop, m.done = p, stop, done
	m.mu.Unlock()
	go func() {
		defer close(done)
		run(p, stop)
	}()
}

// Stop ends the poller, waits for an in-flight poll to be discarded, and withdraws its track.
func (m *Manager) Stop() {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	m.mu.Lock()
	stop, done := m.stop, m.done
	m.provider, m.stop, m.done = nil, nil, nil
	m.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
	// An untitled snapshot replaces the last track at once instead of waiting for it to go stale.
	publishLatest(m.out, player.Track{Source: "spotifyapi", Player: "Spotify", LastUpdated: time.Now()})
}

// Running reports whether a poller is active.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provider != nil
}

// Err is the running poller's last error, if any.
func (m *Manager) Err() string {
	m.mu.Lock()
	p := m.provider
	m.mu.Unlock()
	if p == nil {
		return ""
	}
	return p.Err()
}

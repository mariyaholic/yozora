//go:build windows

package presence

import (
	"errors"
	"strings"
	"time"

	"uika-resonance/internal/discordipc"
)

// TestCardFor is how long a test card stays up before Yozora withdraws it.
const TestCardFor = 15 * time.Second

// ErrTestCardBusy means a test card is already being sent or shown.
var ErrTestCardBusy = errors.New("a test card is already showing")

// TestCard reports the panel's test activity. State is sending, shown, done or failed.
// Confirmed means Discord echoed the activity back when it accepted it.
type TestCard struct {
	State     string    `json:"state"`
	Until     time.Time `json:"until,omitempty"`
	Confirmed bool      `json:"confirmed"`
	Error     string    `json:"error,omitempty"`
}

type testCardState struct {
	requested bool
	card      TestCard
}

// SendTestCard asks the engine loop to publish a labelled test activity on its next step.
// The loop owns the Discord connection, so the request never touches the pipe itself.
func (e *Engine) SendTestCard() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.test.requested || e.test.card.State == "sending" || e.test.card.State == "shown" {
		return ErrTestCardBusy
	}
	e.test.requested = true
	e.test.card = TestCard{State: "sending"}
	return nil
}

func testActivity(smallImage string) *discordipc.Activity {
	return &discordipc.Activity{
		Name:    "Yozora",
		Type:    discordipc.TypeListening,
		Details: "Yozora test card",
		State:   "Checking that your profile shows Yozora",
		Assets:  &discordipc.Assets{LargeImage: smallImage, LargeText: "Test card", SmallImage: smallImage, SmallText: "Yozora"},
	}
}

func confirmedPayload(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "{}"
}

// stepTestCard runs before normal publishing. It returns true while the test card owns Discord.
// When the card expires it is cleared outright: the next normal step republishes the real track,
// so no later gate (debounce, classification, rate limit) can leave the test card standing.
func (e *Engine) stepTestCard(now time.Time) bool {
	e.mu.Lock()
	requested := e.test.requested
	e.test.requested = false
	state, until := e.test.card.State, e.test.card.Until
	e.mu.Unlock()
	if requested {
		return e.publishTestCard(now)
	}
	if state != "shown" {
		return false
	}
	if now.Before(until) {
		return true
	}
	e.mu.Lock()
	e.test.card.State = "done"
	e.test.card.Until = time.Time{}
	e.sentID = ""
	e.sentConfig = ""
	e.mu.Unlock()
	e.clear(now, "")
	e.writeState()
	return false
}

func (e *Engine) publishTestCard(now time.Time) bool {
	cfg := e.Cfg.Get()
	fail := func(msg string) bool {
		e.mu.Lock()
		e.test.card = TestCard{State: "failed", Error: msg}
		e.mu.Unlock()
		e.writeState()
		return false
	}
	clientID := strings.TrimSpace(cfg.Discord.ClientID)
	if clientID == "" {
		return fail("no Discord application ID is configured")
	}
	// A deliberate press deserves a fresh attempt, not the automatic retry backoff.
	e.nextDial.Store(0)
	if !e.ensureIPC(clientID, now) {
		e.mu.Lock()
		msg := e.lastErr
		e.mu.Unlock()
		if msg == "" {
			msg = "Discord did not answer"
		}
		return fail(msg)
	}
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()
	a := testActivity(cfg.Discord.SmallImage)
	if err := ipc.SetActivity(a); err != nil {
		e.setErr("test card: " + err.Error())
		e.disconnect()
		return fail(err.Error())
	}
	payload := ipc.AcceptedPayload()
	e.mu.Lock()
	e.test.card = TestCard{State: "shown", Until: now.Add(TestCardFor), Confirmed: confirmedPayload(payload)}
	// The real card is gone from Discord now; nothing it published is current any more.
	e.published = a
	e.publishedTrack = nil
	e.acknowledged = payload
	e.cleared = false
	e.sentID = ""
	e.lastSend = now
	e.sends++
	e.mu.Unlock()
	e.setErr("")
	e.writeState()
	return true
}

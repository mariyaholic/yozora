//go:build windows

package server

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/player"
	cardtemplate "uika-resonance/internal/template"
)

func TestCardPresetsAreCompleteValidatedAndPreviewWithoutSaving(t *testing.T) {
	h, holder := fixtureHandler(t)
	before, onDisk := holder.Get(), mustLoad(t, holder.Path())
	w := request(h, "GET", "/api/card", "", true)
	var response struct {
		Defaults cardSettings `json:"defaults"`
		Presets  []struct {
			ID       string          `json:"id"`
			Settings json.RawMessage `json:"settings"`
		} `json:"presets"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatalf("GET = %d %s", w.Code, w.Body)
	}
	if len(response.Presets) != 4 || response.Defaults != cardFromConfig(config.Defaults()) {
		t.Fatalf("unexpected preset response: %+v", response)
	}
	want := []struct{ id, details, state, large, small string }{
		{"default", "Night Walk", "Mira", "After Hours", "Spotify"},
		{"compact", "Night Walk", "Mira", "", ""},
		{"album", "After Hours", "Mira", "Night Walk", "Spotify"},
		{"detailed", "Night Walk", "Mira · After Hours", "#3 · After Hours", "Spotify"},
	}
	for i, expected := range want {
		preset := response.Presets[i]
		if preset.ID != expected.id {
			t.Fatalf("preset %d ID = %q, want %q", i, preset.ID, expected.id)
		}
		// Decode through the same required-field and validation path as save/preview.
		settings, err := decodeCard(bytes.NewReader(preset.Settings))
		if err != nil {
			t.Fatalf("%s is not a valid complete form: %v", preset.ID, err)
		}
		if preset.ID == "default" && settings != response.Defaults {
			t.Fatal("default preset differs from Reset defaults")
		}
		preview := request(h, "POST", "/api/card/preview", string(preset.Settings), true)
		var got cardPreview
		if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &got) != nil {
			t.Fatalf("%s preview = %d %s", preset.ID, preview.Code, preview.Body)
		}
		if got.Details != expected.details || got.State != expected.state || got.Large != expected.large || got.Small != expected.small || !got.Sample || !got.Playing {
			t.Errorf("%s preview = %+v", preset.ID, got)
		}
	}
	if holder.Get() != before || !reflect.DeepEqual(mustLoad(t, holder.Path()), onDisk) {
		t.Fatal("reading or previewing presets changed config")
	}
}

func TestCardPresetsHandleMissingAlbumAndPausedTrack(t *testing.T) {
	track := player.Track{Title: "Song", Artist: "Artist", Player: "Player", Playing: true}
	now := time.Now()
	presets := cardPresets()
	album := presets[2].Settings
	engine := cardtemplate.New(album.Details, album.State, album.LargeText, album.SmallText, album.PausedDetails)
	if details, _, _, _ := engine.Render(track, now); details != "Song" {
		t.Fatalf("album preset without album = %q", details)
	}
	detailed := presets[3].Settings
	engine = cardtemplate.New(detailed.Details, detailed.State, detailed.LargeText, detailed.SmallText, detailed.PausedDetails)
	if _, state, large, _ := engine.Render(track, now); state != "Artist" || large != "" {
		t.Fatalf("detailed preset without album or track number = %q, %q", state, large)
	}
	track.TrackNumber = 3
	if _, _, large, _ := engine.Render(track, now); large != "#3" {
		t.Fatalf("detailed preset without album = %q", large)
	}
	track.Playing = false
	if details, _, _, _ := engine.Render(track, now); details != "⏸ Song" {
		t.Fatalf("paused preset = %q", details)
	}
}

//go:build windows

package player

import "testing"

func TestHiddenRuleMatching(t *testing.T) {
	track := Track{Title: "  Night Walk (Live) ", Artist: "Mira"}
	cases := []struct {
		name string
		rule HiddenRule
		want bool
	}{
		{"artist exact", HiddenRule{Artist: "Mira"}, true},
		{"case and space ignored", HiddenRule{Artist: "  mIRA "}, true},
		{"title exact", HiddenRule{Title: "night walk (live)"}, true},
		{"both must match", HiddenRule{Artist: "Mira", Title: "Night Walk (Live)"}, true},
		{"one of two differs", HiddenRule{Artist: "Mira", Title: "Other"}, false},
		{"not a substring", HiddenRule{Artist: "Mir"}, false},
		{"suffix wildcard", HiddenRule{Title: "Night Walk*"}, true},
		{"prefix wildcard", HiddenRule{Title: "* (live)"}, true},
		{"middle wildcard", HiddenRule{Title: "night * (live)"}, true},
		{"wildcard needs the rest", HiddenRule{Title: "night * (remix)"}, false},
		{"wildcard stays ordered", HiddenRule{Title: "walk*night*"}, false},
		{"empty rule matches nothing", HiddenRule{}, false},
		{"wildcard alone matches nothing", HiddenRule{Artist: "*", Title: " * "}, false},
		{"wildcard artist with real title", HiddenRule{Artist: "*", Title: "Night Walk (Live)"}, true},
	}
	for _, c := range cases {
		if got := IsHidden(track, []HiddenRule{c.rule}); got != c.want {
			t.Errorf("%s: IsHidden = %v, want %v", c.name, got, c.want)
		}
	}
	if IsHidden(track, nil) {
		t.Error("no rules hid a track")
	}
	if !IsHidden(track, []HiddenRule{{Artist: "Nobody"}, {Artist: "mira"}}) {
		t.Error("a later rule did not match")
	}
	if IsHidden(Track{Title: "x"}, []HiddenRule{{Artist: "Mira"}}) {
		t.Error("artist rule matched a track with no artist")
	}
}

func TestHiddenRuleSame(t *testing.T) {
	if !(HiddenRule{Artist: " Mira ", Title: "A"}).Same(HiddenRule{Artist: "mira", Title: "a "}) {
		t.Error("equal rules differ")
	}
	if (HiddenRule{Artist: "Mira"}).Same(HiddenRule{Title: "Mira"}) {
		t.Error("artist and title rules compare equal")
	}
}

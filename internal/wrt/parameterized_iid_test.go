//go:build windows

package wrt

import "testing"

// The hot path now hashes only the canonical variant. This pins it to the first
// entry of the probe list so the two can never drift apart.
func TestParameterizedIIDMatchesPrimaryVariant(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		sig     string
		wantLen int
	}{
		{"u32", BaseIAsyncOperation, SigU32(), 4},
		{"smtc manager class", BaseIAsyncOperation, SigClass(
			"Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager",
			"cace8eac-e86e-504a-ab31-5ff8ff1bce49"), 4},
		{"session vector view", BaseIVectorView, SigClass(
			"Windows.Media.Control.GlobalSystemMediaTransportControlsSession",
			"7148c835-9b14-5ae2-ab85-dc9b1c14e1a8"), 4},
		{"interface", BaseIAsyncOperation, SigInterface("905a0fe1-bc53-11df-8c49-001e4fc686da"), 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			variants := ParameterizedIIDVariants(tc.base, tc.sig)
			if len(variants) != tc.wantLen {
				t.Fatalf("variants = %d, want %d", len(variants), tc.wantLen)
			}
			if got := ParameterizedIID(tc.base, tc.sig); got != variants[0] {
				t.Fatalf("ParameterizedIID = %s, want primary variant %s", got, variants[0])
			}
		})
	}
}

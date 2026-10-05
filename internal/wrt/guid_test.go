//go:build windows

package wrt

import "testing"

func TestParseGUIDRoundTrip(t *testing.T) {
	s := "cace8eac-e86e-504a-ab31-5ff8ff1bce49"
	g := MustGUID(s)
	if g.String() != s {
		t.Fatalf("round trip: %s != %s", g.String(), s)
	}
	if g.Data1 != 0xcace8eac || g.Data2 != 0xe86e || g.Data3 != 0x504a {
		t.Fatalf("fields: %v", g)
	}
	if g.Data4[0] != 0xab || g.Data4[7] != 0x49 {
		t.Fatalf("data4: %v", g.Data4)
	}
}

func TestParameterizedIIDStable(t *testing.T) {
	// The convention verified live against the Windows runtime (see probe):
	// mixed-endian namespace, version nibble on byte 6.
	sig := SigClass("Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager",
		"cace8eac-e86e-504a-ab31-5ff8ff1bce49")
	got := ParameterizedIID(BaseIAsyncOperation, sig)
	want := "3eec115e-7346-5c27-8c5f-da78514a277b"
	if got != want {
		t.Fatalf("iid: got %s want %s", got, want)
	}
}

func TestParameterizedIIDDeterministic(t *testing.T) {
	sig := SigU32()
	a := ParameterizedIID(BaseIAsyncOperation, sig)
	b := ParameterizedIID(BaseIAsyncOperation, sig)
	if a != b {
		t.Fatalf("non-deterministic: %s vs %s", a, b)
	}
}

//go:build windows && (amd64 || arm64)

package browserwin

import (
	"testing"
	"unsafe"
)

func TestUIAVariantSize(t *testing.T) {
	if n := unsafe.Sizeof(uiaVariant{}); n != 24 {
		t.Fatalf("VARIANT size = %d, want 24", n)
	}
}

func TestUIABusy(t *testing.T) {
	uiaBusy.Store(true)
	defer uiaBusy.Store(false)
	if _, err := ReadAddressBar(0); err == nil {
		t.Fatal("busy read accepted")
	}
}

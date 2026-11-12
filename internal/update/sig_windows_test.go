//go:build windows

package update

import (
	"os"
	"path/filepath"
	"testing"
)

// The test binary and a text file are unsigned, which is the common case
// (releases are not signed yet): both must read as "not valid" without a
// crash or a hang, so the checksum alone decides.
func TestFileSignatureIsInvalidForUnsignedAndMissingFiles(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	txt := filepath.Join(t.TempDir(), "x.exe")
	os.WriteFile(txt, []byte("not a pe file"), 0o600)
	for _, p := range []string{exe, txt, filepath.Join(t.TempDir(), "missing.exe")} {
		if sig := FileSignature(p); sig.Valid || sig.Subject != "" {
			t.Errorf("FileSignature(%s) = %+v", p, sig)
		}
	}
	if Mode(selfSignature()) != ModeChecksum {
		t.Error("an unsigned running exe must verify by checksum")
	}
}

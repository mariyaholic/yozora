package update

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Signature is the outcome of an Authenticode check on one file.
type Signature struct {
	Valid   bool
	Subject string
}

const (
	// ModeSignature: the running exe is validly signed, so the installer must be
	// signed by the same subject as well as match its checksum.
	ModeSignature = "signature"
	// ModeChecksum: the running exe is not signed, so only the checksum applies.
	ModeChecksum = "checksum"
)

// Mode decides how an installer will be verified from the running exe alone.
func Mode(self Signature) string {
	if self.Valid && self.Subject != "" {
		return ModeSignature
	}
	return ModeChecksum
}

// CheckSignature applies the mode to the downloaded installer's signature.
func CheckSignature(self, installer Signature) error {
	if Mode(self) != ModeSignature {
		return nil
	}
	if !installer.Valid {
		return errors.New("the installer is not validly signed")
	}
	if !strings.EqualFold(installer.Subject, self.Subject) {
		return errors.New("the installer is signed by a different publisher")
	}
	return nil
}

// ParseSums reads a SHA256SUMS.txt ("<hex>  <name>" per line, optional "*"
// before the name for binary mode, with or without a byte order mark).
func ParseSums(b []byte) map[string]string {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	sums := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		hash, name, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		name = strings.TrimPrefix(strings.TrimSpace(name), "*")
		if !ok || name == "" || len(hash) != sha256.Size*2 {
			continue
		}
		if _, err := hex.DecodeString(hash); err != nil {
			continue
		}
		sums[name] = strings.ToLower(hash)
	}
	return sums
}

// VerifyChecksum compares a computed SHA-256 with the one listed for name.
func VerifyChecksum(sums map[string]string, name, got string) error {
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("SHA256SUMS.txt has no entry for %s", name)
	}
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

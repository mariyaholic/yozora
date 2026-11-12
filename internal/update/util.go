package update

import (
	"crypto/rand"
	"encoding/binary"
	"os"
)

func removeAll(dir string) { os.RemoveAll(dir) }

func randInt63n(n int64) int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil || n <= 0 {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(b[:])>>1) % n
}

func selfSignature() Signature {
	exe, err := os.Executable()
	if err != nil {
		return Signature{}
	}
	return FileSignature(exe)
}

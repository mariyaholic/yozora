//go:build windows

// Package wrt provides a minimal, dependency-free Windows Runtime (WinRT) COM
// runtime layer: GUIDs, HSTRINGs, activation factories, vtable calls and
// IAsyncOperation awaiting. Only the interfaces Yozora needs are
// modeled.
package wrt

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// GUID mirrors the Win32 GUID ABI layout (Data1..Data4).
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// ParseGUID parses a canonical GUID string (with or without braces/dashes).
func ParseGUID(s string) (*GUID, error) {
	s = strings.Trim(strings.TrimSpace(s), "{}")
	s = strings.ReplaceAll(strings.ToLower(s), "-", "")
	if len(s) != 32 {
		return nil, fmt.Errorf("wrt: bad guid %q", s)
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("wrt: bad guid %q: %w", s, err)
	}
	g := &GUID{}
	g.Data1 = uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	g.Data2 = uint16(b[4])<<8 | uint16(b[5])
	g.Data3 = uint16(b[6])<<8 | uint16(b[7])
	copy(g.Data4[:], b[8:16])
	return g, nil
}

// MustGUID parses or panics; for compile-time constant GUIDs.
func MustGUID(s string) *GUID {
	g, err := ParseGUID(s)
	if err != nil {
		panic(err)
	}
	return g
}

func (g *GUID) String() string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3],
		g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

// pinterfaceNamespace is the WinRT parameterized-interface namespace UUID
// {d57af411-737b-c042-abae-878b1e16adee} in canonical byte order, as used by
// CsWinRT (wrt_pinterface_namespace) and the Windows metadata rules.
var pinterfaceNamespace = [16]byte{
	0xd5, 0x7a, 0xf4, 0x11, 0x73, 0x7b, 0xc0, 0x42,
	0xab, 0xae, 0x87, 0x8b, 0x1e, 0x16, 0xad, 0xee,
}

// pinterfaceNamespaceMixed is the same namespace in Guid.ToByteArray()
// (mixed-endian) order — one of the two conventions appears in CsWinRT's
// runtime; the live QI probe decides which one matches.
var pinterfaceNamespaceMixed = [16]byte{
	0x11, 0xf4, 0x7a, 0xd5, 0x7b, 0x73, 0x42, 0xc0,
	0xab, 0xae, 0x87, 0x8b, 0x1e, 0x16, 0xad, 0xee,
}

// ParameterizedIID computes the runtime IID of a parameterized (generic)
// interface instantiation per the WinRT rules: UUIDv5 over
// "pinterface({base};<argSig>)" with the pinterface namespace, version 5,
// variant 10xx.
//
// argSignature examples:
//   - runtime class: rc(Windows.Foo.IBar;{default-interface-guid})
//   - interface:     {interface-guid}
//   - UInt32:        u32
func ParameterizedIID(baseGUID, argSignature string) string {
	return ParameterizedIIDVariants(baseGUID, argSignature)[0]
}

// ParameterizedIIDVariants computes candidate IIDs for a parameterized
// instantiation under the possible namespace/bit conventions. The first
// entry is the RFC-4122-correct one; probes verify against the live runtime.
func ParameterizedIIDVariants(baseGUID, argSignature string) []string {
	sig := fmt.Sprintf("pinterface({%s};%s)", strings.ToLower(baseGUID), argSignature)
	mk := func(ns [16]byte, verIdx int) string {
		h := sha1.New()
		h.Write(ns[:])
		h.Write([]byte(sig))
		b := h.Sum(nil)[:16]
		b[verIdx] = (b[verIdx] & 0x0f) | 0x50 // version 5
		b[8] = (b[8] & 0x3f) | 0x80           // variant 10xx
		return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
			b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
			b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
	}
	// Verified against the live Windows runtime: namespace bytes in
	// Guid.ToByteArray() (mixed-endian) order, version nibble on byte 6.
	return []string{
		mk(pinterfaceNamespaceMixed, 6),
		mk(pinterfaceNamespace, 6),
		mk(pinterfaceNamespaceMixed, 7),
		mk(pinterfaceNamespace, 7),
	}
}

// ParameterizedGUID is ParameterizedIID returning a parsed *GUID.
func ParameterizedGUID(baseGUID, argSignature string) *GUID {
	return MustGUID(ParameterizedIID(baseGUID, argSignature))
}

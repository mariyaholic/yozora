//go:build !windows

package update

// FileSignature has no Authenticode off Windows; nothing is ever valid there.
func FileSignature(path string) Signature { return Signature{} }

//go:build windows

package spotifyapi

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
)

func TestClearRefreshTokenIgnoresOnlyMissingCredential(t *testing.T) {
	other := syscall.Errno(5) // ERROR_ACCESS_DENIED
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"deleted", nil, nil},
		{"missing", fmt.Errorf("delete: %w", syscall.Errno(1168)), nil},
		{"denied", fmt.Errorf("delete: %w", other), other},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clearRefreshToken(func(key string) error {
				if key != credKey {
					t.Fatalf("key = %q", key)
				}
				return tc.err
			})
			if tc.want == nil && got != nil || tc.want != nil && !errors.Is(got, tc.want) {
				t.Fatalf("error = %v, want %v", got, tc.want)
			}
		})
	}
}

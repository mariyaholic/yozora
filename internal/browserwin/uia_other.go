//go:build windows && !amd64 && !arm64

package browserwin

import "errors"

// ReadAddressBar is unavailable where by-value VARIANT passing differs.
func ReadAddressBar(hwnd uintptr) (string, error) {
	return "", errors.New("UIA address-bar reading unsupported on this architecture")
}

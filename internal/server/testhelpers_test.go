//go:build windows

package server

import (
	"encoding/json"
)

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

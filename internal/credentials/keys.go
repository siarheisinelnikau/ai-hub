// Package credentials reads private per-model upstream API keys.
package credentials

import (
	"os"
	"path/filepath"
	"strings"
)

func Key(id string) string {
	if id == "" || filepath.Base(id) != id {
		return ""
	}
	dir := os.Getenv("AIHUB_KEYS_DIR")
	if dir == "" {
		dir = "/srv/ai-hub/keys"
	}
	b, err := os.ReadFile(filepath.Join(dir, id))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

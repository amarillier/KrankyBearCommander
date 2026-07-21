package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// uniqueDest returns dir/name, or dir/name (2), dir/name (3), ... if that
// path is already occupied — used when moving a same-named item into a
// shared trash directory that may already hold an item with that name.
func uniqueDest(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if _, err := os.Lstat(candidate); err != nil {
		return candidate
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Lstat(candidate); err != nil {
			return candidate
		}
	}
}

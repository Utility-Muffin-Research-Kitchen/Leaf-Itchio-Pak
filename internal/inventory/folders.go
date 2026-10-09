package inventory

import (
	"path/filepath"
	"strings"
)

// dirInside reports whether dir is a folder below parent. Letter case does
// not count, as on FAT32, and a folder is not inside itself or a folder whose
// name only starts like its own.
func dirInside(dir, parent string) bool {
	child, root := leftOverKey(dir), leftOverKey(parent)
	return child != root && strings.HasPrefix(child, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

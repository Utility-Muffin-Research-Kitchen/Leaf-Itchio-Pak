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

// commonDir is the deepest folder that holds all of paths, or "" when they
// share none. Letter case does not count, as on FAT32; the first path's
// spelling is kept.
func commonDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	separator := string(filepath.Separator)
	common := strings.Split(filepath.Dir(paths[0]), separator)
	for _, path := range paths[1:] {
		parts := strings.Split(filepath.Dir(path), separator)
		shared := 0
		for shared < len(common) && shared < len(parts) && strings.EqualFold(common[shared], parts[shared]) {
			shared++
		}
		common = common[:shared]
	}
	if dir := strings.Join(common, separator); dir != "" && dir != "." {
		return dir
	}
	return ""
}

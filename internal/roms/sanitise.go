package roms

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/text"
)

// SanitiseFilename builds a safe filename from a game title and extension.
// Strips emoji, then strips / : ? * " < > | from the title, trims and collapses whitespace.
// Returns "" when title is empty or reduces to empty after stripping (caller should use the upstream filename instead).
func SanitiseFilename(title, ext string) string {
	if title == "" {
		return ""
	}
	title = text.StripEmoji(title)
	const strip = `/:?*"<>|`
	var b strings.Builder
	for _, r := range title {
		if strings.ContainsRune(strip, r) {
			continue
		}
		b.WriteRune(r)
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	if s == "" {
		return ""
	}
	return s + ext
}

// ResolveUnifiedDest returns the desired on-disk path for a ROM after applying
// unified naming. currentPath is where the file was written. gameTitle is the
// itch.io game title (used to derive the target filename).
//
// Returns (currentPath, false) when no rename is needed (name already correct,
// or title is empty). Returns (targetPath, true) when a rename is required.
//
// If allowOverwrite is true (download context), the returned path may already
// exist on disk — the caller's os.Rename will atomically replace it. Exception:
// if currentPath is already a numbered slot for this game (e.g. "Title (2).gb"),
// the slot is preserved and (currentPath, false) is returned so a re-download
// does not overwrite a different game occupying the primary name.
//
// If allowOverwrite is false (migration context), appends " (2)", " (3)" etc.
// to avoid colliding with any pre-existing file.
func ResolveUnifiedDest(currentPath, gameTitle string, allowOverwrite bool) (string, bool) {
	ext := ROMExt(filepath.Base(currentPath))
	candidate := SanitiseFilename(gameTitle, ext)
	if candidate == "" || strings.EqualFold(candidate, filepath.Base(currentPath)) {
		return currentPath, false
	}
	dir := filepath.Dir(currentPath)
	target := filepath.Join(dir, candidate)
	if existing, exists := existingCaseFoldPath(dir, candidate); exists && existing != currentPath {
		if allowOverwrite {
			stem := strings.TrimSuffix(candidate, ext)
			if isNumberedSlot(filepath.Base(currentPath), stem, ext) {
				return currentPath, false
			}
			// Use the existing path's real casing. This gives case-sensitive test
			// hosts the same collision behavior as the FAT32 target filesystem.
			target = existing
		} else {
			stem := strings.TrimSuffix(candidate, ext)
			for n := 2; ; n++ {
				candidate = fmt.Sprintf("%s (%d)%s", stem, n, ext)
				target = filepath.Join(dir, candidate)
				if _, exists := existingCaseFoldPath(dir, candidate); !exists {
					break
				}
				if target == currentPath {
					return currentPath, false
				}
			}
		}
	}
	return target, target != currentPath
}

// existingCaseFoldPath returns the real path of an entry whose basename is a
// Unicode case-fold match. Leaf content lives on FAT32, where case-only names
// collide; using the same rule on case-sensitive development hosts keeps
// collision planning deterministic.
func existingCaseFoldPath(dir, base string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), base) {
			return filepath.Join(dir, entry.Name()), true
		}
	}
	return "", false
}

// isNumberedSlot reports whether base matches the pattern "stem (N)ext" for
// some non-empty digit sequence N. Used to detect that a file was deliberately
// placed in a collision slot and should not be moved to the primary name.
func isNumberedSlot(base, stem, ext string) bool {
	prefix := stem + " ("
	suffix := ")" + ext
	if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, suffix) {
		return false
	}
	mid := base[len(prefix) : len(base)-len(suffix)]
	if mid == "" {
		return false
	}
	for _, c := range mid {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// UnifiedTarget returns the path unified naming would give path for
// gameTitle, ignoring what exists on disk, or "" when no rename applies.
func UnifiedTarget(path, gameTitle string) string {
	candidate := SanitiseFilename(gameTitle, ROMExt(filepath.Base(path)))
	if candidate == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), candidate)
}

// UnifiedCollisions reports which of paths must keep their original names
// because unified naming would give them the same final name as another
// file of the same operation. Unified naming derives the name from the game
// title, so two ROMs of one game with one extension in one folder always
// meet; there is no single right name for them, and their original names are
// what tells them apart. Names are compared case-insensitively, as FAT32
// does. A target that equals another file's original name also collides.
func UnifiedCollisions(paths []string, gameTitle string) []bool {
	targets := make([]string, len(paths))
	for index, path := range paths {
		targets[index] = UnifiedTarget(path, gameTitle)
	}
	keep := make([]bool, len(paths))
	for index, target := range targets {
		if target == "" || sameFAT32Path(target, paths[index]) {
			continue
		}
		for other := range paths {
			if other != index && (sameFAT32Path(target, targets[other]) || sameFAT32Path(target, paths[other])) {
				keep[index] = true
				break
			}
		}
	}
	return keep
}

// NameReservations tracks the final paths one download operation has
// written or is about to write, so nothing inside the operation replaces
// another of its own files. Replacing a file from an earlier download (an
// intentional re-download) is unaffected. Paths are compared
// case-insensitively, as FAT32 does.
type NameReservations struct {
	paths []string
}

// Claim reserves path. It reports false, reserving nothing, when the
// operation already holds a path FAT32 would treat as the same file.
func (r *NameReservations) Claim(path string) bool {
	if r.Holds(path) {
		return false
	}
	r.paths = append(r.paths, filepath.Clean(path))
	return true
}

// Holds reports whether the operation has reserved path.
func (r *NameReservations) Holds(path string) bool {
	for _, held := range r.paths {
		if sameFAT32Path(held, path) {
			return true
		}
	}
	return false
}

// Release drops a reservation, for a file the operation renamed away.
func (r *NameReservations) Release(path string) {
	for index, held := range r.paths {
		if sameFAT32Path(held, path) {
			r.paths = append(r.paths[:index], r.paths[index+1:]...)
			return
		}
	}
}

func sameFAT32Path(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

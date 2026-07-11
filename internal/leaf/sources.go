package leaf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Source struct {
	ID         string
	Root       string
	Primary    bool
	RomsPath   string
	ImagesPath string
	MusicPath  string
	AppsPath   string
	BIOSPath   string
	SavesPath  string
	StatesPath string
	CheatsPath string
}

func (s Source) Available() bool {
	info, err := os.Stat(s.Root)
	return err == nil && info.IsDir()
}

type SourceList []Source

func (s SourceList) Primary() (Source, bool) {
	if len(s) == 0 {
		return Source{}, false
	}
	return s[0], true
}

func (s SourceList) ByID(id string) (Source, bool) {
	for _, source := range s {
		if source.ID == id {
			return source, true
		}
	}
	return Source{}, false
}

func resolveRootList(getenv getenvFunc, primary, secondary string) ([]string, error) {
	if raw, ok := getenv("SDCARD_PATHS"); ok && raw != "" {
		roots, err := parsePathList("SDCARD_PATHS", raw)
		if err != nil {
			return nil, err
		}
		if roots[0] != primary {
			return nil, fmt.Errorf("SDCARD_PATHS primary %q does not match SDCARD_PATH %q", roots[0], primary)
		}
		return roots, nil
	}
	if secondary != "" && secondary != primary {
		return []string{primary, secondary}, nil
	}
	return []string{primary}, nil
}

func parsePathList(name, raw string) ([]string, error) {
	parts := strings.Split(raw, ":")
	if len(parts) == 0 {
		return nil, fmt.Errorf("%s is empty", name)
	}
	seen := make(map[string]bool, len(parts))
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			return nil, fmt.Errorf("%s contains an empty item at index %d", name, i)
		}
		parts[i] = cleanPath(part)
		if seen[parts[i]] {
			return nil, fmt.Errorf("%s contains duplicate root %q", name, parts[i])
		}
		seen[parts[i]] = true
	}
	return parts, nil
}

func resolveContentPaths(getenv getenvFunc, roots []string, plural, singular, child string) ([]string, error) {
	if raw, ok := getenv(plural); ok && raw != "" {
		paths, err := parsePathList(plural, raw)
		if err != nil {
			return nil, err
		}
		if len(paths) != len(roots) {
			return nil, fmt.Errorf("%s has %d items; SDCARD_PATHS has %d", plural, len(paths), len(roots))
		}
		if value, ok := getenv(singular); ok && value != "" && cleanPath(value) != paths[0] {
			return nil, fmt.Errorf("%s primary %q does not match %s %q", plural, paths[0], singular, cleanPath(value))
		}
		return paths, nil
	}
	paths := make([]string, len(roots))
	for i, root := range roots {
		paths[i] = filepath.Join(root, child)
	}
	if value, ok := getenv(singular); ok && value != "" {
		paths[0] = cleanPath(value)
	}
	return paths, nil
}

func resolveSources(getenv getenvFunc, roots []string, secondary string) (SourceList, error) {
	type contentSpec struct {
		plural, singular, child string
	}
	specs := []contentSpec{
		{"ROMS_PATHS", "ROMS_PATH", "Roms"},
		{"IMAGES_PATHS", "IMAGES_PATH", "Images"},
		{"MUSIC_PATHS", "MUSIC_PATH", "Music"},
		{"APPS_PATHS", "APPS_PATH", "Apps"},
		{"BIOS_PATHS", "BIOS_PATH", "BIOS"},
		{"SAVES_PATHS", "SAVES_PATH", "Saves"},
		{"STATES_PATHS", "STATES_PATH", "States"},
		{"CHEATS_PATHS", "CHEATS_PATH", "Cheats"},
	}
	resolved := make([][]string, len(specs))
	for i, spec := range specs {
		paths, err := resolveContentPaths(getenv, roots, spec.plural, spec.singular, spec.child)
		if err != nil {
			return nil, err
		}
		resolved[i] = paths
	}

	sources := make(SourceList, len(roots))
	for i, root := range roots {
		id := fmt.Sprintf("source%d", i+1)
		if i == 0 {
			id = "primary"
		} else if root == secondary {
			id = "secondary_sd"
		}
		sources[i] = Source{
			ID: id, Root: root, Primary: i == 0,
			RomsPath: resolved[0][i], ImagesPath: resolved[1][i],
			MusicPath: resolved[2][i], AppsPath: resolved[3][i],
			BIOSPath: resolved[4][i], SavesPath: resolved[5][i],
			StatesPath: resolved[6][i], CheatsPath: resolved[7][i],
		}
	}
	return sources, nil
}

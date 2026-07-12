//go:build !headless

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type catDestinationTarget struct {
	key, label, legacyExt string
}

type CatDestinationFlow struct {
	sources leaf.SourceList
	catalog *leaf.Catalog
	cfg     *settings.Config
	cfgPath string
	title   string
	music   bool

	targets       []catDestinationTarget
	uploads       []roms.Upload
	uploadTargets []int
	selected      leaf.Source
	targetIndex   int
	root          string
	current       string
	chosenDirs    map[string]string
	destPaths     []string
	archiveExts   map[string][]string
	preferences   map[string]settings.RememberedDestination
}

func NewCatROMDestinationFlow(sources leaf.SourceList, catalog *leaf.Catalog,
	cfg *settings.Config, cfgPath, title string, uploads []roms.Upload) (*CatDestinationFlow, *appui.DestinationModel, error) {
	exts := make([]string, len(uploads))
	for index, upload := range uploads {
		exts[index] = strings.ToLower(roms.ROMExt(upload.Filename))
	}
	return newCatROMDestinationFlow(sources, catalog, cfg, cfgPath, title, uploads, exts, false)
}

// NewCatLogicalROMDestinationFlow chooses destinations using the inspected
// inner ROM extensions while retaining the outer upload filenames.
func NewCatLogicalROMDestinationFlow(sources leaf.SourceList, catalog *leaf.Catalog,
	cfg *settings.Config, cfgPath, title string, uploads []roms.Upload,
	exts []string) (*CatDestinationFlow, *appui.DestinationModel, error) {
	return newCatROMDestinationFlow(sources, catalog, cfg, cfgPath, title, uploads, exts, false)
}

// NewCatArchiveROMDestinationFlow chooses one directory per distinct ROM
// system represented in an inspected archive.
func NewCatArchiveROMDestinationFlow(sources leaf.SourceList, catalog *leaf.Catalog,
	cfg *settings.Config, cfgPath, title string,
	exts []string) (*CatDestinationFlow, *appui.DestinationModel, error) {
	uploads := make([]roms.Upload, len(exts))
	return newCatROMDestinationFlow(sources, catalog, cfg, cfgPath, title, uploads, exts, true)
}

func newCatROMDestinationFlow(sources leaf.SourceList, catalog *leaf.Catalog,
	cfg *settings.Config, cfgPath, title string, uploads []roms.Upload,
	exts []string, archive bool) (*CatDestinationFlow, *appui.DestinationModel, error) {
	if len(uploads) != len(exts) {
		return nil, nil, fmt.Errorf("download destination metadata is inconsistent")
	}
	flow := &CatDestinationFlow{
		sources: sources, catalog: catalog, cfg: cfg, cfgPath: cfgPath, title: title,
		uploads:    append([]roms.Upload(nil), uploads...),
		chosenDirs: make(map[string]string), uploadTargets: make([]int, len(uploads)),
		archiveExts: make(map[string][]string), preferences: make(map[string]settings.RememberedDestination),
	}
	byKey := make(map[string]int)
	for index, ext := range exts {
		ext = strings.ToLower(ext)
		canonical, ok := leaf.CanonicalSystemForExtension(ext)
		if !ok || canonical == "GBC" && ext == ".zip" {
			return nil, nil, fmt.Errorf("cannot choose a ROM destination for %q", ext)
		}
		targetIndex, exists := byKey[canonical]
		if !exists {
			targetIndex = len(flow.targets)
			byKey[canonical] = targetIndex
			flow.targets = append(flow.targets, catDestinationTarget{key: canonical, label: canonical, legacyExt: ext})
		}
		flow.uploadTargets[index] = targetIndex
		flow.archiveExts[canonical] = append(flow.archiveExts[canonical], ext)
	}
	if len(flow.targets) == 0 {
		return nil, nil, fmt.Errorf("download has no destination targets")
	}
	model := appui.NewDestinationModel(title)
	flow.showSources(model)
	if !archive {
		flow.archiveExts = nil
	}
	return flow, model, nil
}

func NewCatMusicDestinationFlow(sources leaf.SourceList, cfg *settings.Config,
	cfgPath, title string) (*CatDestinationFlow, *appui.DestinationModel, error) {
	if len(sources) == 0 {
		return nil, nil, fmt.Errorf("Leaf runtime has no storage sources")
	}
	flow := &CatDestinationFlow{
		sources: sources, cfg: cfg, cfgPath: cfgPath, title: title, music: true,
		targets:       []catDestinationTarget{{key: "music", label: "Music"}},
		uploadTargets: []int{0}, chosenDirs: make(map[string]string),
		preferences: make(map[string]settings.RememberedDestination),
	}
	model := appui.NewDestinationModel(title)
	flow.showSources(model)
	return flow, model, nil
}

func (flow *CatDestinationFlow) showSources(model *appui.DestinationModel) {
	items := make([]appui.DestinationItem, 0, len(flow.sources))
	for index, source := range flow.sources {
		label := fmt.Sprintf("SD card %d", index+1)
		if source.Primary {
			label = "Primary SD"
		} else if source.ID == "secondary_sd" {
			label = "Secondary SD"
		}
		available := source.Available()
		detail := "Available"
		if !available {
			detail = "Not mounted"
		}
		items = append(items, appui.DestinationItem{
			Kind: appui.DestinationItemSource, Label: label, Detail: detail,
			Value: source.ID, Enabled: available,
		})
	}
	model.SetSources(items)
}

func (flow *CatDestinationFlow) Activate(model *appui.DestinationModel) (bool, error) {
	if model.Phase == appui.DestinationConfirm {
		return flow.finalize(model)
	}
	if model.Cursor < 0 || model.Cursor >= len(model.Items) || !model.Items[model.Cursor].Enabled {
		return false, nil
	}
	item := model.Items[model.Cursor]
	if model.Phase == appui.DestinationSources {
		source, ok := flow.sources.ByID(item.Value)
		if !ok || !source.Available() {
			return false, fmt.Errorf("selected storage card is no longer mounted")
		}
		flow.selected = source
		flow.targetIndex = 0
		flow.chosenDirs = make(map[string]string)
		flow.preferences = make(map[string]settings.RememberedDestination)
		return false, flow.openTarget(model)
	}
	if model.Phase != appui.DestinationFolders {
		return false, nil
	}
	switch item.Kind {
	case appui.DestinationItemSave:
		return flow.confirm(model)
	case appui.DestinationItemUp:
		return false, flow.loadDir(model, filepath.Dir(flow.current))
	case appui.DestinationItemFolder:
		child, err := leaf.JoinWithin(flow.current, item.Value)
		if err != nil {
			return false, err
		}
		return false, flow.loadDir(model, child)
	}
	return false, nil
}

// Back returns true only when the whole picker should be cancelled.
func (flow *CatDestinationFlow) Back(model *appui.DestinationModel) bool {
	if model.Phase == appui.DestinationSources {
		return true
	}
	if model.Phase == appui.DestinationError {
		flow.showSources(model)
		return false
	}
	if model.Phase == appui.DestinationConfirm {
		flow.targetIndex = len(flow.targets) - 1
		flow.current = flow.chosenDirs[flow.targets[flow.targetIndex].key]
		_ = flow.loadDir(model, flow.current)
		return false
	}
	if filepath.Clean(flow.current) != filepath.Clean(flow.root) {
		_ = flow.loadDir(model, filepath.Dir(flow.current))
		return false
	}
	flow.targetIndex = 0
	flow.chosenDirs = make(map[string]string)
	flow.preferences = make(map[string]settings.RememberedDestination)
	flow.showSources(model)
	return false
}

func (flow *CatDestinationFlow) DestPaths() []string {
	return append([]string(nil), flow.destPaths...)
}

// ArchiveROMDirs returns the chosen directory keyed by each inspected inner
// extension, ready for ZIPPlan.ROMDirs.
func (flow *CatDestinationFlow) ArchiveROMDirs() map[string]string {
	dirs := make(map[string]string)
	for canonical, exts := range flow.archiveExts {
		for _, ext := range exts {
			dirs[ext] = flow.chosenDirs[canonical]
		}
	}
	return dirs
}

func (flow *CatDestinationFlow) openTarget(model *appui.DestinationModel) error {
	target := flow.targets[flow.targetIndex]
	var err error
	if flow.music {
		flow.root = flow.selected.MusicPath
	} else {
		flow.root, err = flow.catalog.ROMDir(flow.selected, target.key)
		if err != nil {
			return err
		}
	}
	start := flow.rememberedStart(target)
	return flow.loadDir(model, start)
}

func (flow *CatDestinationFlow) rememberedStart(target catDestinationTarget) string {
	preference := settings.RememberedDestination{}
	found := false
	if flow.music {
		if flow.cfg.MusicDestination != nil {
			preference, found = *flow.cfg.MusicDestination, true
		}
	} else if flow.cfg.ROMDestinations != nil {
		preference, found = flow.cfg.ROMDestinations[target.key]
	}
	if found && preference.SourceID == flow.selected.ID {
		if candidate, err := leaf.JoinWithin(flow.root, preference.RelativePath); err == nil && catDestinationDirectorySafe(flow.root, candidate, false) == nil {
			return candidate
		}
	}
	// One-way compatibility for the old absolute primary-only preference.
	if !flow.music && flow.selected.Primary && flow.cfg.LastROMDirs != nil {
		if candidate := flow.cfg.LastROMDirs[target.legacyExt]; candidate != "" && catDestinationDirectorySafe(flow.root, candidate, false) == nil {
			return candidate
		}
	}
	return flow.root
}

func (flow *CatDestinationFlow) loadDir(model *appui.DestinationModel, dir string) error {
	if err := catDestinationDirectorySafe(flow.root, dir, false); err != nil {
		if filepath.Clean(dir) != filepath.Clean(flow.root) || !os.IsNotExist(err) {
			return err
		}
	}
	flow.current = filepath.Clean(dir)
	items := []appui.DestinationItem{{
		Kind: appui.DestinationItemSave, Label: "Save here", Detail: "Use this folder", Enabled: true,
	}}
	if flow.current != filepath.Clean(flow.root) {
		items = append(items, appui.DestinationItem{Kind: appui.DestinationItemUp, Label: "..", Detail: "Parent folder", Enabled: true})
	}
	entries, err := os.ReadDir(flow.current)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read destination folder: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	for _, name := range names {
		items = append(items, appui.DestinationItem{
			Kind: appui.DestinationItemFolder, Label: name, Detail: "Folder", Value: name, Enabled: true,
		})
	}
	rel, _ := leaf.RelativeWithin(flow.root, flow.current)
	pathLabel := destinationSourceLabel(flow.selected)
	if rel != "." && rel != "" {
		pathLabel += " / " + filepath.ToSlash(rel)
	}
	target := flow.targets[flow.targetIndex]
	subtitle := fmt.Sprintf("Choose %s folder", target.label)
	if len(flow.targets) > 1 {
		subtitle = fmt.Sprintf("Choose %s folder (%d/%d)", target.label, flow.targetIndex+1, len(flow.targets))
	}
	model.SetFolders(subtitle, pathLabel, items)
	return nil
}

func (flow *CatDestinationFlow) confirm(model *appui.DestinationModel) (bool, error) {
	if !flow.selected.Available() {
		return false, fmt.Errorf("selected storage card was removed")
	}
	if err := catDestinationDirectorySafe(flow.root, flow.current, false); err != nil {
		return false, err
	}
	target := flow.targets[flow.targetIndex]
	rel, err := leaf.RelativeWithin(flow.root, flow.current)
	if err != nil {
		return false, err
	}
	preference := settings.RememberedDestination{SourceID: flow.selected.ID, RelativePath: filepath.ToSlash(rel)}
	flow.preferences[target.key] = preference
	flow.chosenDirs[target.key] = flow.current
	if flow.targetIndex+1 < len(flow.targets) {
		flow.targetIndex++
		return false, flow.openTarget(model)
	}
	flow.destPaths = make([]string, len(flow.uploadTargets))
	for uploadIndex, targetIndex := range flow.uploadTargets {
		flow.destPaths[uploadIndex] = flow.chosenDirs[flow.targets[targetIndex].key]
	}
	model.SetConfirm("Confirm download destination", destinationSourceLabel(flow.selected), flow.summaryLines())
	return false, nil
}

func (flow *CatDestinationFlow) finalize(_ *appui.DestinationModel) (bool, error) {
	if !flow.selected.Available() {
		return false, fmt.Errorf("selected storage card was removed")
	}
	for _, target := range flow.targets {
		dir := flow.chosenDirs[target.key]
		root := flow.selected.MusicPath
		if !flow.music {
			var err error
			root, err = flow.catalog.ROMDir(flow.selected, target.key)
			if err != nil {
				return false, err
			}
		}
		if err := catDestinationDirectorySafe(root, dir, true); err != nil {
			return false, fmt.Errorf("selected storage card changed before download: %w", err)
		}
	}
	if flow.music {
		preference := flow.preferences["music"]
		flow.cfg.MusicDestination = &preference
	} else {
		if flow.cfg.ROMDestinations == nil {
			flow.cfg.ROMDestinations = make(map[string]settings.RememberedDestination)
		}
		for key, preference := range flow.preferences {
			flow.cfg.ROMDestinations[key] = preference
		}
	}
	if err := flow.cfg.Save(flow.cfgPath); err != nil {
		logger.Warn("destination: save preference: %v", err)
	}
	return true, nil
}

func (flow *CatDestinationFlow) summaryLines() []string {
	var lines []string
	if flow.music {
		rel, _ := leaf.RelativeWithin(flow.selected.Root, flow.chosenDirs["music"])
		return []string{filepath.ToSlash(rel)}
	}
	for targetIndex, target := range flow.targets {
		dir := flow.chosenDirs[target.key]
		rel, _ := leaf.RelativeWithin(flow.selected.Root, dir)
		added := false
		for uploadIndex, mappedTarget := range flow.uploadTargets {
			if mappedTarget != targetIndex || uploadIndex >= len(flow.uploads) {
				continue
			}
			if flow.uploads[uploadIndex].Filename != "" {
				lines = append(lines, filepath.ToSlash(filepath.Join(rel, flow.uploads[uploadIndex].Filename)))
				added = true
			}
		}
		label := filepath.ToSlash(rel)
		if !added || len(flow.uploadTargets) == 0 {
			label = target.label + " → " + label
		}
		lines = append(lines, label)
	}
	return lines
}

func catDestinationDirectorySafe(root, target string, create bool) error {
	if _, err := leaf.RelativeWithin(root, target); err != nil {
		return err
	}
	rootExisting, err := nearestExistingDirectory(root)
	if err != nil {
		return err
	}
	targetExisting, err := nearestExistingDirectory(target)
	if err != nil {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(rootExisting)
	if err != nil {
		return err
	}
	targetReal, err := filepath.EvalSymlinks(targetExisting)
	if err != nil {
		return err
	}
	if _, err := leaf.RelativeWithin(rootReal, targetReal); err != nil {
		return fmt.Errorf("destination follows a symlink outside its content root: %w", err)
	}
	if create {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("create destination folder: %w", err)
		}
		return catDestinationDirectorySafe(root, target, false)
	}
	return nil
}

func nearestExistingDirectory(path string) (string, error) {
	for candidate := filepath.Clean(path); ; candidate = filepath.Dir(candidate) {
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("destination ancestor %q is not a directory", candidate)
			}
			return candidate, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", err
		}
	}
}

func destinationSourceLabel(source leaf.Source) string {
	if source.Primary {
		return "Primary SD"
	}
	if source.ID == "secondary_sd" {
		return "Secondary SD"
	}
	return source.ID
}

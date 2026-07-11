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
	uploadTargets []int
	selected      leaf.Source
	targetIndex   int
	root          string
	current       string
	chosenDirs    map[string]string
	destPaths     []string
}

func NewCatROMDestinationFlow(sources leaf.SourceList, catalog *leaf.Catalog,
	cfg *settings.Config, cfgPath, title string, uploads []roms.Upload) (*CatDestinationFlow, *appui.DestinationModel, error) {
	flow := &CatDestinationFlow{
		sources: sources, catalog: catalog, cfg: cfg, cfgPath: cfgPath, title: title,
		chosenDirs: make(map[string]string), uploadTargets: make([]int, len(uploads)),
	}
	byKey := make(map[string]int)
	for index, upload := range uploads {
		ext := strings.ToLower(roms.ROMExt(upload.Filename))
		canonical, ok := leaf.CanonicalSystemForExtension(ext)
		if !ok || canonical == "GBC" && ext == ".zip" {
			return nil, nil, fmt.Errorf("cannot choose a ROM destination for %q before archive inspection", upload.Filename)
		}
		targetIndex, exists := byKey[canonical]
		if !exists {
			targetIndex = len(flow.targets)
			byKey[canonical] = targetIndex
			flow.targets = append(flow.targets, catDestinationTarget{key: canonical, label: canonical, legacyExt: ext})
		}
		flow.uploadTargets[index] = targetIndex
	}
	if len(flow.targets) == 0 {
		return nil, nil, fmt.Errorf("download has no destination targets")
	}
	model := appui.NewDestinationModel(title)
	flow.showSources(model)
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
	if filepath.Clean(flow.current) != filepath.Clean(flow.root) {
		_ = flow.loadDir(model, filepath.Dir(flow.current))
		return false
	}
	flow.targetIndex = 0
	flow.chosenDirs = make(map[string]string)
	flow.showSources(model)
	return false
}

func (flow *CatDestinationFlow) DestPaths() []string {
	return append([]string(nil), flow.destPaths...)
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
	if err := catDestinationDirectorySafe(flow.root, flow.current, true); err != nil {
		return false, err
	}
	target := flow.targets[flow.targetIndex]
	rel, err := leaf.RelativeWithin(flow.root, flow.current)
	if err != nil {
		return false, err
	}
	preference := settings.RememberedDestination{SourceID: flow.selected.ID, RelativePath: filepath.ToSlash(rel)}
	if flow.music {
		flow.cfg.MusicDestination = &preference
	} else {
		if flow.cfg.ROMDestinations == nil {
			flow.cfg.ROMDestinations = make(map[string]settings.RememberedDestination)
		}
		flow.cfg.ROMDestinations[target.key] = preference
	}
	if err := flow.cfg.Save(flow.cfgPath); err != nil {
		logger.Warn("destination: save preference: %v", err)
	}
	flow.chosenDirs[target.key] = flow.current
	if flow.targetIndex+1 < len(flow.targets) {
		flow.targetIndex++
		return false, flow.openTarget(model)
	}
	flow.destPaths = make([]string, len(flow.uploadTargets))
	for uploadIndex, targetIndex := range flow.uploadTargets {
		flow.destPaths[uploadIndex] = flow.chosenDirs[flow.targets[targetIndex].key]
	}
	return true, nil
}

func catDestinationDirectorySafe(root, target string, create bool) error {
	if _, err := leaf.RelativeWithin(root, target); err != nil {
		return err
	}
	if create {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("create destination folder: %w", err)
		}
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	if _, err := leaf.RelativeWithin(rootReal, targetReal); err != nil {
		return fmt.Errorf("destination follows a symlink outside its content root: %w", err)
	}
	return nil
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

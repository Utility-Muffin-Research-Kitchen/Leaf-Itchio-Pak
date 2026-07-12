//go:build !headless

package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

type CatManageFlow struct {
	inv                *inventory.Inventory
	inventoryPath      string
	gameURL            string
	sources            leaf.SourceList
	catalog            *leaf.Catalog
	entry              inventory.Entry
	pending            []int
	libraryScanPending bool
}

func NewCatManageFlow(inv *inventory.Inventory, inventoryPath, gameURL string,
	sources leaf.SourceList, catalog *leaf.Catalog) (*CatManageFlow, *appui.ManageModel, error) {
	flow := &CatManageFlow{inv: inv, inventoryPath: inventoryPath, gameURL: gameURL, sources: sources, catalog: catalog}
	entry, ok := inv.Lookup(gameURL)
	if !ok || len(entry.Files) == 0 {
		return nil, nil, fmt.Errorf("downloaded files are no longer present in the inventory")
	}
	model := appui.NewManageModel(entry.Title)
	flow.refresh(model)
	return flow, model, nil
}

func (flow *CatManageFlow) refresh(model *appui.ManageModel) {
	entry, ok := flow.inv.Lookup(flow.gameURL)
	if !ok {
		flow.entry = inventory.Entry{}
		model.SetItems("No managed files remain", nil)
		return
	}
	flow.entry = entry
	items := make([]appui.ManageItem, 0, len(entry.Files)*2+3)
	romIndices, musicIndices := []int{}, []int{}
	for index, file := range entry.Files {
		kind := managedContentKind(file)
		if kind == inventory.ContentKindMusic {
			musicIndices = append(musicIndices, index)
		} else {
			romIndices = append(romIndices, index)
		}
		_, rel, sourceErr := flow.resolveFile(file, false)
		badge := strings.ToUpper(kind)
		enabled := sourceErr == nil
		if !enabled {
			badge = "UNAVAILABLE"
		}
		label := file.InstalledName
		if label == "" {
			label = filepath.Base(file.DestPath)
		}
		items = append(items, appui.ManageItem{
			Kind: appui.ManageItemFile, Label: label, Detail: rel, Badge: badge,
			FileIndex: index, Enabled: enabled,
		})
	}
	if len(romIndices) > 0 {
		items = append(items, appui.ManageItem{
			Kind: appui.ManageItemDeleteROMs, Label: "Delete ROM files", Badge: fmt.Sprintf("%d ROM", len(romIndices)),
			Enabled: flow.indicesAvailable(romIndices),
		})
	}
	if len(musicIndices) > 0 {
		items = append(items, appui.ManageItem{
			Kind: appui.ManageItemDeleteMusic, Label: "Delete soundtrack", Badge: fmt.Sprintf("%d MUSIC", len(musicIndices)),
			Enabled: flow.indicesAvailable(musicIndices),
		})
	}
	items = append(items, appui.ManageItem{
		Kind: appui.ManageItemDeleteAll, Label: "Delete all downloads", Badge: fmt.Sprintf("%d FILES", len(entry.Files)),
		Enabled: flow.indicesAvailable(allFileIndices(entry.Files)),
	})
	for _, index := range romIndices {
		file := entry.Files[index]
		if !roms.SupportsUnifiedNaming(file.DestPath) {
			continue
		}
		label := "Use title for " + filepath.Base(file.DestPath)
		if file.UnifiedName {
			label = "Restore upload name for " + filepath.Base(file.DestPath)
		}
		items = append(items, appui.ManageItem{
			Kind: appui.ManageItemRename, Label: label, Badge: "RENAME",
			FileIndex: index, Enabled: flow.indicesAvailable([]int{index}),
		})
	}
	model.SetItems(fmt.Sprintf("%d managed file(s) · source-owned paths only", len(entry.Files)), items)
}

func (flow *CatManageFlow) indicesAvailable(indices []int) bool {
	for _, index := range indices {
		if index < 0 || index >= len(flow.entry.Files) {
			return false
		}
		if _, _, err := flow.resolveFile(flow.entry.Files[index], false); err != nil {
			return false
		}
	}
	return len(indices) > 0
}

func (flow *CatManageFlow) Activate(model *appui.ManageModel) (*CatRenameFlow, *appui.RenameModel, error) {
	if model.Cursor < 0 || model.Cursor >= len(model.Items) {
		return nil, nil, nil
	}
	item := model.Items[model.Cursor]
	if !item.Enabled {
		err := flow.itemUnavailableError(item)
		model.SetError(err.Error())
		return nil, nil, nil
	}
	if item.Kind == appui.ManageItemRename {
		return NewCatRenameFlow(flow.inv, flow.inventoryPath, flow.gameURL, item.FileIndex, flow.sources)
	}
	var indices []int
	switch item.Kind {
	case appui.ManageItemFile:
		indices = []int{item.FileIndex}
	case appui.ManageItemDeleteROMs:
		indices = flow.indicesByKind(inventory.ContentKindROM)
	case appui.ManageItemDeleteMusic:
		indices = flow.indicesByKind(inventory.ContentKindMusic)
	case appui.ManageItemDeleteAll:
		indices = allFileIndices(flow.entry.Files)
	default:
		return nil, nil, nil
	}
	flow.pending = append([]int(nil), indices...)
	lines := make([]string, 0, len(indices)*2)
	for _, index := range indices {
		file := flow.entry.Files[index]
		_, rel, _ := flow.resolveFile(file, false)
		lines = append(lines, filepath.Base(file.DestPath), rel)
	}
	title := "Delete selected file?"
	if len(indices) > 1 {
		title = fmt.Sprintf("Delete %d managed files?", len(indices))
	}
	model.SetConfirm(title, lines)
	return nil, nil, nil
}

func (flow *CatManageFlow) itemUnavailableError(item appui.ManageItem) error {
	var indices []int
	switch item.Kind {
	case appui.ManageItemFile, appui.ManageItemRename:
		indices = []int{item.FileIndex}
	case appui.ManageItemDeleteROMs:
		indices = flow.indicesByKind(inventory.ContentKindROM)
	case appui.ManageItemDeleteMusic:
		indices = flow.indicesByKind(inventory.ContentKindMusic)
	case appui.ManageItemDeleteAll:
		indices = allFileIndices(flow.entry.Files)
	}
	for _, index := range indices {
		if index < 0 || index >= len(flow.entry.Files) {
			return fmt.Errorf("inventory changed before this action")
		}
		if _, _, err := flow.resolveFile(flow.entry.Files[index], false); err != nil {
			return err
		}
	}
	return fmt.Errorf("this action is currently unavailable")
}

func (flow *CatManageFlow) Cancel(model *appui.ManageModel) { flow.pending = nil; flow.refresh(model) }

// Back returns true when the management route should return to Detail.
func (flow *CatManageFlow) Back(model *appui.ManageModel) bool {
	if model.State == appui.ManageConfirm {
		flow.Cancel(model)
		return false
	}
	if model.State == appui.ManageResult || model.State == appui.ManageError {
		if _, ok := flow.inv.Lookup(flow.gameURL); ok {
			flow.refresh(model)
			return false
		}
	}
	return true
}

func (flow *CatManageFlow) Confirm(model *appui.ManageModel) (bool, error) {
	flow.libraryScanPending = false
	if len(flow.pending) == 0 {
		return false, fmt.Errorf("no files were selected")
	}
	files := make([]inventory.DownloadedFile, 0, len(flow.pending))
	for _, index := range flow.pending {
		if index < 0 || index >= len(flow.entry.Files) {
			return false, fmt.Errorf("inventory changed before deletion")
		}
		file := flow.entry.Files[index]
		if _, _, err := flow.resolveFile(file, true); err != nil {
			return false, err
		}
		files = append(files, file)
	}
	lease, err := leaf.BeginOperation(context.Background(), "delete batch", false)
	if err != nil {
		return false, fmt.Errorf("protect delete batch: %w", err)
	}
	defer lease.Release()

	deleted := make([]inventory.DownloadedFile, 0, len(files))
	var deleteErr error
	for _, file := range files {
		if err := os.Remove(file.DestPath); err != nil && !os.IsNotExist(err) {
			deleteErr = fmt.Errorf("delete %s: %w", filepath.Base(file.DestPath), err)
			break
		}
		deleted = append(deleted, file)
	}
	for _, file := range deleted {
		flow.removeOwnedArtwork(file, deleted)
		flow.pruneManagedDir(file)
	}
	for _, file := range deleted {
		flow.inv.RemoveFile(flow.gameURL, file.DestPath)
	}
	if len(deleted) > 0 {
		if err := flow.inv.Save(flow.inventoryPath); err != nil && deleteErr == nil {
			deleteErr = fmt.Errorf("save inventory after deletion: %w", err)
		}
	}
	flow.pending = nil
	_, stillPresent := flow.inv.Lookup(flow.gameURL)
	if deleteErr != nil {
		flow.refresh(model)
		model.SetError(deleteErr.Error())
		return !stillPresent, deleteErr
	}
	model.SetResult(fmt.Sprintf("Deleted %d managed file(s).", len(deleted)))
	for _, file := range deleted {
		if managedContentKind(file) == inventory.ContentKindROM {
			flow.libraryScanPending = true
			break
		}
	}
	return !stillPresent, nil
}

// TakeLibraryScanRequest consumes the single Jawaka scan required by one
// committed deletion batch, regardless of how many ROM files it contained.
func (flow *CatManageFlow) TakeLibraryScanRequest() bool {
	pending := flow.libraryScanPending
	flow.libraryScanPending = false
	return pending
}

func (flow *CatManageFlow) indicesByKind(kind string) []int {
	var indices []int
	for index, file := range flow.entry.Files {
		if managedContentKind(file) == kind {
			indices = append(indices, index)
		}
	}
	return indices
}

func (flow *CatManageFlow) resolveFile(file inventory.DownloadedFile, requireFile bool) (leaf.Source, string, error) {
	identity := roms.PathIdentity{SourceID: file.SourceID, RelativePath: file.RelativePath, CanonicalSystem: file.CanonicalSystem}
	if identity.SourceID == "" || identity.RelativePath == "" {
		var ok bool
		identity, ok = roms.DescribeDestination(file.DestPath)
		if !ok {
			return leaf.Source{}, "Outside configured sources", fmt.Errorf("%s is outside configured Leaf sources", filepath.Base(file.DestPath))
		}
	}
	source, ok := flow.sources.ByID(identity.SourceID)
	if !ok {
		return leaf.Source{}, identity.RelativePath, fmt.Errorf("storage source %q is unknown", identity.SourceID)
	}
	label := destinationSourceLabel(source) + " / " + filepath.ToSlash(identity.RelativePath)
	if !source.Available() {
		return source, label, fmt.Errorf("%s is not mounted", destinationSourceLabel(source))
	}
	expected, err := leaf.JoinWithin(source.Root, identity.RelativePath)
	if err != nil || filepath.Clean(expected) != filepath.Clean(file.DestPath) {
		return source, label, fmt.Errorf("inventory path identity does not match %s", filepath.Base(file.DestPath))
	}
	if err := catDestinationDirectorySafe(source.Root, filepath.Dir(file.DestPath), false); err != nil {
		return source, label, fmt.Errorf("unsafe managed path: %w", err)
	}
	if requireFile {
		info, err := os.Lstat(file.DestPath)
		if err != nil {
			return source, label, fmt.Errorf("managed file is unavailable: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return source, label, fmt.Errorf("managed path is not a regular file")
		}
	}
	return source, label, nil
}

func (flow *CatManageFlow) removeOwnedArtwork(file inventory.DownloadedFile, deleting []inventory.DownloadedFile) {
	if !file.ArtworkCreated || managedContentKind(file) != inventory.ContentKindROM {
		return
	}
	artPath := inventory.ArtworkPathFor(flow.entry.CoverURL, file)
	if artPath == "" || flow.inv.ArtworkReferencedOutside(artPath, deleting) {
		return
	}
	source, _, err := flow.resolveFile(file, false)
	if err != nil {
		return
	}
	if _, err := leaf.RelativeWithin(source.Root, artPath); err != nil {
		return
	}
	_ = os.Remove(artPath)
	if filepath.Base(filepath.Dir(artPath)) == ".media" {
		_ = os.Remove(filepath.Dir(artPath))
	}
}

func (flow *CatManageFlow) pruneManagedDir(file inventory.DownloadedFile) {
	source, _, err := flow.resolveFile(file, false)
	if err != nil {
		return
	}
	root := source.MusicPath
	if managedContentKind(file) == inventory.ContentKindROM && file.CanonicalSystem != "" {
		if systemRoot, rootErr := flow.catalog.ROMDir(source, file.CanonicalSystem); rootErr == nil {
			root = systemRoot
		}
	}
	dir := filepath.Dir(file.DestPath)
	for filepath.Clean(dir) != filepath.Clean(root) {
		if _, err := leaf.RelativeWithin(root, dir); err != nil || os.Remove(dir) != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
}

func managedContentKind(file inventory.DownloadedFile) string {
	if file.ContentKind == inventory.ContentKindMusic || file.FileType == inventory.FileTypeMusic {
		return inventory.ContentKindMusic
	}
	return inventory.ContentKindROM
}

func allFileIndices(files []inventory.DownloadedFile) []int {
	indices := make([]int, len(files))
	for index := range files {
		indices[index] = index
	}
	return indices
}

type renamePair struct{ oldPath, newPath string }

type CatRenameFlow struct {
	inv                *inventory.Inventory
	inventoryPath      string
	gameURL            string
	entry              inventory.Entry
	file               inventory.DownloadedFile
	source             leaf.Source
	enable             bool
	targetPath         string
	saves, states      []renamePair
	renameSaves        bool
	renameStates       bool
	libraryScanPending bool
}

func NewCatRenameFlow(inv *inventory.Inventory, inventoryPath, gameURL string, fileIndex int,
	sources leaf.SourceList) (*CatRenameFlow, *appui.RenameModel, error) {
	entry, ok := inv.Lookup(gameURL)
	if !ok || fileIndex < 0 || fileIndex >= len(entry.Files) {
		return nil, nil, fmt.Errorf("managed ROM is no longer in the inventory")
	}
	file := entry.Files[fileIndex]
	if !roms.SupportsUnifiedNaming(file.DestPath) {
		return nil, nil, fmt.Errorf("this PlayStation descriptor or companion file must keep its original name")
	}
	if managedContentKind(file) != inventory.ContentKindROM {
		return nil, nil, fmt.Errorf("only ROM files can be renamed")
	}
	identity := roms.PathIdentity{SourceID: file.SourceID, RelativePath: file.RelativePath, CanonicalSystem: file.CanonicalSystem}
	if identity.SourceID == "" || identity.RelativePath == "" {
		var described bool
		identity, described = roms.DescribeDestination(file.DestPath)
		if !described {
			return nil, nil, fmt.Errorf("ROM path is outside configured Leaf sources")
		}
	}
	source, ok := sources.ByID(identity.SourceID)
	if !ok || !source.Available() {
		return nil, nil, fmt.Errorf("the ROM's storage card is not mounted")
	}
	expected, err := leaf.JoinWithin(source.Root, identity.RelativePath)
	if err != nil || filepath.Clean(expected) != filepath.Clean(file.DestPath) {
		return nil, nil, fmt.Errorf("ROM path does not match its recorded source identity")
	}
	flow := &CatRenameFlow{
		inv: inv, inventoryPath: inventoryPath, gameURL: gameURL, entry: entry,
		file: file, source: source, enable: !file.UnifiedName,
	}
	if flow.enable {
		flow.targetPath, _ = roms.ResolveUnifiedDest(file.DestPath, entry.Title, false)
	} else {
		name := filepath.Base(file.Filename)
		if name == "." || name == string(filepath.Separator) || name == "" {
			return nil, nil, fmt.Errorf("original upload name is not safe")
		}
		flow.targetPath = filepath.Join(filepath.Dir(file.DestPath), name)
	}
	if filepath.Clean(flow.targetPath) == filepath.Clean(file.DestPath) {
		return nil, nil, fmt.Errorf("the ROM already has the requested filename")
	}
	if _, err := leaf.RelativeWithin(source.Root, flow.targetPath); err != nil {
		return nil, nil, fmt.Errorf("rename target escapes the selected source: %w", err)
	}
	if _, err := os.Lstat(flow.targetPath); err == nil {
		return nil, nil, fmt.Errorf("rename target already exists: %s", filepath.Base(flow.targetPath))
	} else if !os.IsNotExist(err) {
		return nil, nil, err
	}
	flow.saves, err = discoverRenamePairs(source.SavesPath, filepath.Base(file.DestPath), filepath.Base(flow.targetPath), false)
	if err != nil {
		return nil, nil, err
	}
	flow.states, err = discoverRenamePairs(source.StatesPath, filepath.Base(file.DestPath), filepath.Base(flow.targetPath), true)
	if err != nil {
		return nil, nil, err
	}
	model := appui.NewRenameModel(entry.Title)
	model.SetPrompt(appui.RenameConfirmROM, destinationSourceLabel(source), "Rename ROM file?",
		flow.displayPairs([]renamePair{{oldPath: file.DestPath, newPath: flow.targetPath}}))
	return flow, model, nil
}

func (flow *CatRenameFlow) Confirm(model *appui.RenameModel) error {
	switch model.State {
	case appui.RenameConfirmROM:
		return flow.advance(model)
	case appui.RenameConfirmSaves:
		flow.renameSaves = true
		return flow.advance(model)
	case appui.RenameConfirmStates:
		flow.renameStates = true
		return flow.execute(model)
	}
	return nil
}

func (flow *CatRenameFlow) Skip(model *appui.RenameModel) error {
	switch model.State {
	case appui.RenameConfirmSaves:
		return flow.advance(model)
	case appui.RenameConfirmStates:
		return flow.execute(model)
	}
	return nil
}

func (flow *CatRenameFlow) advance(model *appui.RenameModel) error {
	if model.State == appui.RenameConfirmROM && len(flow.saves) > 0 {
		model.SetPrompt(appui.RenameConfirmSaves, "Save files", "Rename these save files?", flow.displayPairs(flow.saves))
		return nil
	}
	if (model.State == appui.RenameConfirmROM || model.State == appui.RenameConfirmSaves) && len(flow.states) > 0 {
		model.SetPrompt(appui.RenameConfirmStates, "Save states", "Rename these state files?", flow.displayPairs(flow.states))
		return nil
	}
	return flow.execute(model)
}

func (flow *CatRenameFlow) execute(model *appui.RenameModel) error {
	if !flow.source.Available() {
		return fmt.Errorf("the ROM's storage card was removed")
	}
	pairs := []renamePair{{oldPath: flow.file.DestPath, newPath: flow.targetPath}}
	if flow.renameSaves {
		pairs = append(pairs, flow.saves...)
	}
	if flow.renameStates {
		pairs = append(pairs, flow.states...)
	}
	if flow.file.ArtworkCreated {
		oldArt := inventory.ArtworkPathFor(flow.entry.CoverURL, flow.file)
		newArt := inventory.CanonicalArtworkPath(flow.targetPath)
		if oldArt != "" {
			if _, err := os.Lstat(oldArt); err == nil {
				pairs = append(pairs, renamePair{oldPath: oldArt, newPath: newArt})
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	for _, pair := range pairs {
		if _, err := leaf.RelativeWithin(flow.source.Root, pair.oldPath); err != nil {
			return fmt.Errorf("rename source escapes selected card: %w", err)
		}
		if _, err := leaf.RelativeWithin(flow.source.Root, pair.newPath); err != nil {
			return fmt.Errorf("rename target escapes selected card: %w", err)
		}
		if info, err := os.Lstat(pair.oldPath); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("rename source is unavailable or unsafe: %s", filepath.Base(pair.oldPath))
		}
		if _, err := os.Lstat(pair.newPath); err == nil {
			return fmt.Errorf("rename target already exists: %s", filepath.Base(pair.newPath))
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	lease, err := leaf.BeginOperation(context.Background(), "rename batch", false)
	if err != nil {
		return fmt.Errorf("protect rename batch: %w", err)
	}
	defer lease.Release()
	completed := make([]renamePair, 0, len(pairs))
	rollback := func() {
		for index := len(completed) - 1; index >= 0; index-- {
			_ = os.Rename(completed[index].newPath, completed[index].oldPath)
		}
	}
	for _, pair := range pairs {
		if err := os.Rename(pair.oldPath, pair.newPath); err != nil {
			rollback()
			return fmt.Errorf("rename %s: %w", filepath.Base(pair.oldPath), err)
		}
		completed = append(completed, pair)
	}
	updated := flow.file
	updated.DestPath = flow.targetPath
	updated.InstalledName = filepath.Base(flow.targetPath)
	updated.UnifiedName = flow.enable
	if flow.file.ArtworkCreated && flow.file.ArtworkPath != "" {
		updated.ArtworkPath = inventory.CanonicalArtworkPath(flow.targetPath)
	}
	if identity, ok := roms.DescribeDestination(flow.targetPath); ok {
		updated.SourceID, updated.RelativePath, updated.CanonicalSystem = identity.SourceID, identity.RelativePath, identity.CanonicalSystem
	}
	if !flow.inv.UpdateFile(flow.gameURL, flow.file.DestPath, updated) {
		rollback()
		return fmt.Errorf("inventory changed during rename")
	}
	flow.inv.SetUnifiedNamingDisabled(flow.gameURL, !flow.enable)
	if err := flow.inv.Save(flow.inventoryPath); err != nil {
		flow.inv.UpdateFile(flow.gameURL, flow.targetPath, flow.file)
		flow.inv.SetUnifiedNamingDisabled(flow.gameURL, flow.entry.UnifiedNamingDisabled)
		rollback()
		return fmt.Errorf("commit renamed inventory: %w", err)
	}
	flow.libraryScanPending = true
	parts := []string{"ROM renamed"}
	if flow.renameSaves {
		parts = append(parts, fmt.Sprintf("%d save(s)", len(flow.saves)))
	}
	if flow.renameStates {
		parts = append(parts, fmt.Sprintf("%d state file(s)", len(flow.states)))
	}
	model.SetDone(strings.Join(parts, ", ") + ".")
	return nil
}

// TakeLibraryScanRequest consumes the single Jawaka scan required by the
// committed ROM/save/state/artwork rename transaction.
func (flow *CatRenameFlow) TakeLibraryScanRequest() bool {
	pending := flow.libraryScanPending
	flow.libraryScanPending = false
	return pending
}

func (flow *CatRenameFlow) displayPairs(pairs []renamePair) []string {
	lines := make([]string, 0, len(pairs)*2)
	for _, pair := range pairs {
		oldRel, _ := leaf.RelativeWithin(flow.source.Root, pair.oldPath)
		newRel, _ := leaf.RelativeWithin(flow.source.Root, pair.newPath)
		lines = append(lines, filepath.ToSlash(oldRel), "→ "+filepath.ToSlash(newRel))
	}
	return lines
}

func discoverRenamePairs(root, oldBase, newBase string, states bool) ([]renamePair, error) {
	if root == "" {
		return nil, nil
	}
	oldStem := strings.TrimSuffix(oldBase, roms.ROMExt(oldBase))
	newStem := strings.TrimSuffix(newBase, roms.ROMExt(newBase))
	dirs := []string{root}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && !strings.HasPrefix(entry.Name(), ".") {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	var pairs []renamePair
	seenTargets := make(map[string]string)
	for _, dir := range dirs {
		files, readErr := os.ReadDir(dir)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range files {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			newName, match := renameCandidate(entry.Name(), oldBase, oldStem, newBase, newStem, states)
			if !match {
				continue
			}
			oldPath, newPath := filepath.Join(dir, entry.Name()), filepath.Join(dir, newName)
			key := strings.ToLower(filepath.Clean(newPath))
			if prior, exists := seenTargets[key]; exists && prior != oldPath {
				return nil, fmt.Errorf("ambiguous rename candidates target %s", newName)
			}
			seenTargets[key] = oldPath
			if _, statErr := os.Lstat(newPath); statErr == nil && filepath.Clean(newPath) != filepath.Clean(oldPath) {
				return nil, fmt.Errorf("related-file target already exists: %s", newName)
			} else if statErr != nil && !os.IsNotExist(statErr) {
				return nil, statErr
			}
			if filepath.Clean(newPath) != filepath.Clean(oldPath) {
				pairs = append(pairs, renamePair{oldPath: oldPath, newPath: newPath})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].oldPath < pairs[j].oldPath })
	return pairs, nil
}

func renameCandidate(name, oldBase, oldStem, newBase, newStem string, states bool) (string, bool) {
	matchPrefix := func(old, replacement string) (string, bool) {
		if len(name) < len(old) || !strings.EqualFold(name[:len(old)], old) {
			return "", false
		}
		return replacement + name[len(old):], true
	}
	validSaveSuffix := func(suffix string) bool {
		return strings.EqualFold(suffix, ".sav") || strings.EqualFold(suffix, ".srm")
	}
	validStateSuffix := func(suffix string) bool {
		lower := strings.ToLower(suffix)
		thumbnail := strings.HasSuffix(lower, ".png")
		if thumbnail {
			lower = strings.TrimSuffix(lower, ".png")
		}
		if lower == ".state" || lower == ".state.auto" {
			return true
		}
		for _, prefix := range []string{".state", ".state."} {
			if strings.HasPrefix(lower, prefix) {
				digits := strings.TrimPrefix(lower, prefix)
				if digits != "" && strings.Trim(digits, "0123456789") == "" {
					return true
				}
			}
		}
		return false
	}
	for _, candidate := range []struct{ old, replacement string }{{oldBase, newBase}, {oldStem, newStem}} {
		newName, ok := matchPrefix(candidate.old, candidate.replacement)
		if !ok {
			continue
		}
		suffix := name[len(candidate.old):]
		if (!states && validSaveSuffix(suffix)) || (states && validStateSuffix(suffix)) {
			return newName, true
		}
	}
	return "", false
}

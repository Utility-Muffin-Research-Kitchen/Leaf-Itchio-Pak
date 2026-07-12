//go:build !headless

package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type CatArchiveAction uint8

const (
	CatArchiveNone CatArchiveAction = iota
	CatArchiveChooseContents
	CatArchiveChooseROMDestination
	CatArchiveChooseMusicDestination
	CatArchiveStartDirect
	CatArchiveStartExtraction
)

type catArchiveUpdate struct {
	plan ZIPPlan
	err  error
}

// CatArchiveFlow owns archive inspection and the choices required to turn its
// manifest into either a direct ZIP download or a safe extraction plan. It has
// no renderer dependency; Cat screens only observe its appui models.
type CatArchiveFlow struct {
	client *itchio.Client
	cfg    *settings.Config
	game   itchio.Game
	upload roms.Upload
	inv    *inventory.Inventory
	wake   func()

	fetched atomic.Int64
	total   atomic.Int64
	updates chan catArchiveUpdate
	plan    ZIPPlan
	action  CatArchiveAction
	direct  *CatDownloadPlan

	choiceExts     []string
	choiceIndex    int
	skipROMChoices bool
}

func NewCatArchiveFlow(client *itchio.Client, cfg *settings.Config, game itchio.Game,
	upload roms.Upload, inv *inventory.Inventory, wake func()) *CatArchiveFlow {
	flow := &CatArchiveFlow{client: client, cfg: cfg, game: game, upload: upload, inv: inv,
		wake: wake, updates: make(chan catArchiveUpdate, 1)}
	go flow.inspect()
	return flow
}

func (flow *CatArchiveFlow) inspect() {
	var cdnURL string
	var err error
	if flow.upload.DownloadKeyID != "" {
		cdnURL, err = flow.client.ResolveAuthURL(flow.cfg.APIKey, flow.upload.UploadID, flow.upload.DownloadKeyID)
	} else {
		cdnURL, err = flow.client.ResolveFreeURL(itchio.Upload{Filename: flow.upload.Filename, URL: flow.upload.URL})
	}
	if err != nil {
		flow.publish(catArchiveUpdate{err: err})
		return
	}
	progress := func(fetched, total int64) {
		flow.fetched.Store(fetched)
		flow.total.Store(total)
		if flow.wake != nil {
			flow.wake()
		}
	}
	var manifest roms.ZIPManifest
	if strings.EqualFold(filepath.Ext(flow.upload.Filename), ".7z") {
		manifest, err = roms.InspectRemote7z(flow.client.HTTPClient(), cdnURL)
	} else {
		manifest, err = roms.InspectRemoteZIP(flow.client.HTTPClient(), cdnURL, progress)
	}
	flow.publish(catArchiveUpdate{plan: ZIPPlan{Upload: flow.upload, CDNURL: cdnURL, Manifest: manifest}, err: err})
}

func (flow *CatArchiveFlow) publish(update catArchiveUpdate) {
	flow.updates <- update
	if flow.wake != nil {
		flow.wake()
	}
}

func (flow *CatArchiveFlow) Snapshot() appui.DownloadProgressModel {
	return appui.DownloadProgressModel{State: appui.DownloadProgressRunning,
		Title: flow.game.Title, Filename: "Inspecting " + flow.upload.Filename,
		Downloaded: flow.fetched.Load(), Total: flow.total.Load(), FileCount: 1}
}

func (flow *CatArchiveFlow) Sync(model *appui.DownloadProgressModel) bool {
	model.Downloaded, model.Total = flow.fetched.Load(), flow.total.Load()
	select {
	case update := <-flow.updates:
		if update.err != nil {
			model.State = appui.DownloadProgressError
			model.Detail = update.err.Error()
			return true
		}
		flow.plan = update.plan
		if err := ValidateArchiveManifest(flow.plan.Manifest, DefaultArchiveLimits); err != nil {
			model.State = appui.DownloadProgressError
			model.Detail = err.Error()
			return true
		}
		if !flow.plan.Manifest.HasROMs() && !flow.plan.Manifest.HasMusic() {
			model.State = appui.DownloadProgressError
			model.Detail = "The archive contains no supported ROM or music files."
			return true
		}
		if !flow.plan.Manifest.HasROMs() && flow.cfg.MusicDownload == "off" {
			model.State = appui.DownloadProgressError
			model.Detail = "This archive only contains music, and soundtrack downloads are disabled in Settings."
			return true
		}
		flow.prepareInitialAction()
		return true
	default:
		return false
	}
}

func (flow *CatArchiveFlow) TakeAction() CatArchiveAction {
	action := flow.action
	flow.action = CatArchiveNone
	return action
}

func (flow *CatArchiveFlow) TakeDirectPlan() *CatDownloadPlan {
	plan := flow.direct
	flow.direct = nil
	return plan
}

func (flow *CatArchiveFlow) ExtractionPlan() ZIPPlan { return flow.plan.Seal() }

func (flow *CatArchiveFlow) ROMExtensions() []string {
	exts := make([]string, 0, len(flow.plan.Manifest.InstallROMExts()))
	for _, ext := range flow.plan.Manifest.InstallROMExts() {
		if flow.plan.SelectedROMs != nil {
			if selected, ok := flow.plan.SelectedROMs[ext]; ok && selected == "" {
				continue
			}
		}
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

func (flow *CatArchiveFlow) PrepareChoices(model *appui.DownloadSelectModel) {
	flow.choiceExts = nil
	for ext, entries := range flow.plan.Manifest.ROMsByExt() {
		if flow.skipROMChoices {
			break
		}
		if len(entries) > 1 {
			flow.choiceExts = append(flow.choiceExts, ext)
		}
	}
	sort.Strings(flow.choiceExts)
	flow.choiceIndex = 0
	if flow.plan.SelectedROMs == nil {
		flow.plan.SelectedROMs = make(map[string]string)
		for ext, entries := range flow.plan.Manifest.ROMsByExt() {
			if len(entries) == 1 {
				flow.plan.SelectedROMs[ext] = entries[0].Name
			}
		}
	}
	flow.showNextChoice(model)
}

func (flow *CatArchiveFlow) Choose(model *appui.DownloadSelectModel) {
	if model.Cursor < 0 || model.Cursor >= len(model.Choices) {
		return
	}
	if flow.choiceIndex < len(flow.choiceExts) {
		ext := flow.choiceExts[flow.choiceIndex]
		entries := flow.plan.Manifest.ROMsByExt()[ext]
		if model.Cursor < len(entries) {
			flow.plan.SelectedROMs[ext] = entries[model.Cursor].Name
		}
		flow.choiceIndex++
	} else if flow.plan.Manifest.HasMusic() && flow.cfg.MusicDownload == "ask" {
		flow.plan.DownloadMusic = model.Cursor == 0
		flow.choiceIndex++
	}
	flow.showNextChoice(model)
}

func (flow *CatArchiveFlow) showNextChoice(model *appui.DownloadSelectModel) {
	if flow.choiceIndex < len(flow.choiceExts) {
		ext := flow.choiceExts[flow.choiceIndex]
		entries := flow.plan.Manifest.ROMsByExt()[ext]
		choices := make([]appui.DownloadChoice, 0, len(entries))
		for _, entry := range entries {
			choices = append(choices, appui.DownloadChoice{Title: entry.Name, Badge: strings.TrimPrefix(strings.ToUpper(ext), ".")})
		}
		model.SetChoices(fmt.Sprintf("Choose one %s ROM (%d/%d)", strings.ToUpper(ext), flow.choiceIndex+1, len(flow.choiceExts)), choices)
		return
	}
	if flow.choiceIndex == len(flow.choiceExts) && flow.plan.Manifest.HasMusic() && flow.cfg.MusicDownload == "ask" {
		model.SetChoices(fmt.Sprintf("Archive contains %d soundtrack file(s)", flow.plan.Manifest.MusicCount()),
			[]appui.DownloadChoice{{Title: "Download soundtrack", Badge: "YES"}, {Title: "Skip soundtrack", Badge: "NO"}})
		return
	}
	flow.plan.DownloadROMs = flow.plan.Manifest.HasROMs()
	if flow.cfg.MusicDownload == "auto" && flow.plan.Manifest.HasMusic() {
		flow.plan.DownloadMusic = true
	}
	flow.finishDestinations()
}

func (flow *CatArchiveFlow) SetROMDirs(dirs map[string]string) {
	flow.plan.ROMDirs = dirs
	if flow.plan.Pico8GameDir != "" {
		base := dirs[".p8"]
		if base == "" {
			base = dirs[".p8.png"]
		}
		name := roms.SanitiseFilename(flow.game.Title, "")
		if name == "" {
			name = "Unknown"
		}
		flow.plan.Pico8GameDir = filepath.Join(base, name) + string(filepath.Separator)
	}
	flow.finishMusicDestination()
}

func (flow *CatArchiveFlow) SetMusicDir(dir string) {
	flow.plan.MusicDir = dir
	flow.action = CatArchiveStartExtraction
}

func (flow *CatArchiveFlow) prepareInitialAction() {
	m := flow.plan.Manifest
	if m.HasPSXFiles() {
		flow.plan.DownloadROMs = true
		flow.skipROMChoices = true
		if m.HasMusic() && flow.cfg.MusicDownload == "ask" {
			flow.action = CatArchiveChooseContents
			return
		}
		flow.plan.DownloadMusic = m.HasMusic() && flow.cfg.MusicDownload == "auto"
		flow.finishDestinations()
		return
	}
	if m.IsSingleROMOnly() && !m.HasOtherFiles() {
		ext := flow.firstROMExt()
		if ext != ".p8" && ext != ".p8.png" && !strings.EqualFold(filepath.Ext(flow.upload.Filename), ".7z") {
			patched := flow.upload
			patched.URL = flow.plan.CDNURL
			flow.direct = &CatDownloadPlan{Kind: CatDownloadPlanDirect, Uploads: []roms.Upload{patched}}
			if flow.cfg.ROMLocation == "ask" {
				flow.direct.Kind = CatDownloadPlanDestination
				flow.direct.LogicalExts = []string{ext}
			} else {
				dest := roms.DestinationDir(ext, flow.cfg.Pico8Core) + patched.Filename
				if existing := flow.inv.ExistingDestPath(flow.game.URL, patched.Filename); existing != "" {
					dest = existing
				}
				flow.direct.DestPaths = []string{dest}
			}
			flow.action = CatArchiveStartDirect
			return
		}
		flow.plan.DownloadROMs = true
		flow.finishDestinations()
		return
	}

	byExt := m.ROMsByExt()
	if png := byExt[".p8.png"]; len(png) == 1 && len(byExt[".p8"]) > 0 {
		flow.plan.DownloadROMs = true
		flow.plan.SelectedROMs = map[string]string{".p8.png": png[0].Name, ".p8": ""}
		flow.skipROMChoices = true
		if m.HasMusic() && flow.cfg.MusicDownload == "ask" {
			flow.action = CatArchiveChooseContents
			return
		}
		flow.plan.DownloadMusic = m.HasMusic() && flow.cfg.MusicDownload == "auto"
		flow.finishDestinations()
		return
	}
	if m.IsPico8MultiFileGame() {
		flow.plan.DownloadROMs = true
		flow.plan.Pico8GameDir = roms.Pico8GameSubDir(flow.cfg.Pico8Core, flow.game.Title)
		flow.skipROMChoices = true
		if m.HasMusic() && flow.cfg.MusicDownload == "ask" {
			flow.action = CatArchiveChooseContents
			return
		}
		flow.plan.DownloadMusic = m.HasMusic() && flow.cfg.MusicDownload == "auto"
		flow.finishDestinations()
		return
	}
	if m.HasDuplicateROMExt() || (flow.cfg.MusicDownload == "ask" && m.HasMusic()) {
		flow.action = CatArchiveChooseContents
		return
	}
	flow.plan.DownloadROMs = m.HasROMs()
	flow.plan.DownloadMusic = m.HasMusic() && flow.cfg.MusicDownload == "auto"
	flow.finishDestinations()
}

func (flow *CatArchiveFlow) finishDestinations() {
	if flow.plan.DownloadROMs && flow.cfg.ROMLocation == "ask" {
		flow.action = CatArchiveChooseROMDestination
		return
	}
	flow.finishMusicDestination()
}

func (flow *CatArchiveFlow) finishMusicDestination() {
	if flow.plan.DownloadMusic {
		if flow.cfg.MusicLocation == "ask" {
			flow.action = CatArchiveChooseMusicDestination
			return
		}
		flow.plan.MusicDir = roms.MusicDestinationDir(flow.game.Title)
	}
	flow.action = CatArchiveStartExtraction
}

func (flow *CatArchiveFlow) firstROMExt() string {
	for _, entry := range flow.plan.Manifest.Entries {
		if entry.Kind == roms.KindROM {
			return strings.ToLower(roms.ROMExt(entry.Name))
		}
	}
	return ""
}

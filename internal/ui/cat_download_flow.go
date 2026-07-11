//go:build !headless

package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type CatDownloadPlanKind uint8

const (
	CatDownloadPlanNone CatDownloadPlanKind = iota
	CatDownloadPlanDirect
	CatDownloadPlanMulti
	CatDownloadPlanArchive
	CatDownloadPlanDestination
)

type CatDownloadPlan struct {
	Kind      CatDownloadPlanKind
	Uploads   []roms.Upload
	DestPaths []string
}

type catDownloadMode uint8

const (
	catDownloadModeNone catDownloadMode = iota
	catDownloadModePurchases
	catDownloadModeUploads
	catDownloadModeFormats
)

type catDownloadUpdateKind uint8

const (
	catDownloadUpdatePurchases catDownloadUpdateKind = iota
	catDownloadUpdateUploads
	catDownloadUpdateDetected
)

type catDownloadUpdate struct {
	kind    catDownloadUpdateKind
	keys    []itchio.OwnedKey
	uploads []roms.Upload
	upload  roms.Upload
	ext     string
	err     error
}

type CatDownloadFlow struct {
	client *itchio.Client
	cfg    *settings.Config
	game   itchio.Game
	detail *itchio.GameDetail
	inv    *inventory.Inventory
	wake   func()

	mode    catDownloadMode
	keys    []itchio.OwnedKey
	uploads []roms.Upload
	updates chan catDownloadUpdate
	plan    *CatDownloadPlan
}

func NewCatDownloadFlow(client *itchio.Client, cfg *settings.Config, game itchio.Game,
	detail *itchio.GameDetail, inv *inventory.Inventory, wake func()) *CatDownloadFlow {
	flow := &CatDownloadFlow{
		client: client, cfg: cfg, game: game, detail: detail, inv: inv, wake: wake,
		updates: make(chan catDownloadUpdate, 2),
	}
	flow.discover()
	return flow
}

func (flow *CatDownloadFlow) discover() {
	go func() {
		update := catDownloadUpdate{}
		if !flow.game.IsFree && flow.cfg.APIKey != "" && flow.detail != nil && flow.detail.GameID != "" {
			keys, err := flow.client.FetchOwnedKeys(flow.cfg.APIKey, flow.detail.GameID)
			if err != nil {
				update.err = err
			} else if len(keys) > 1 {
				update.kind = catDownloadUpdatePurchases
				update.keys = itchio.AnnotateBundleNames(keys, flow.detail.BundleNames)
			} else if len(keys) == 1 {
				update = flow.fetchForKey(keys[0])
			} else {
				update.err = fmt.Errorf("game is not owned by the configured itch.io account")
			}
		} else {
			uploads, err := flow.client.FetchUploads(flow.game.URL)
			update.kind, update.err = catDownloadUpdateUploads, err
			for _, upload := range uploads {
				update.uploads = append(update.uploads, roms.Upload{
					Filename: upload.Filename, URL: upload.URL, NeedsFormat: upload.NeedsFormat,
				})
			}
		}
		flow.publish(update)
	}()
}

func (flow *CatDownloadFlow) fetchForKey(key itchio.OwnedKey) catDownloadUpdate {
	downloadKeyID := strconv.FormatInt(key.ID, 10)
	uploads, err := flow.client.FetchUploadsForKey(flow.cfg.APIKey, flow.detail.GameID, downloadKeyID)
	update := catDownloadUpdate{kind: catDownloadUpdateUploads, err: err}
	for _, upload := range uploads {
		update.uploads = append(update.uploads, roms.Upload{
			Filename: upload.Filename, UploadID: upload.UploadID,
			DownloadKeyID: downloadKeyID, NeedsFormat: upload.NeedsFormat,
		})
	}
	return update
}

func (flow *CatDownloadFlow) publish(update catDownloadUpdate) {
	flow.updates <- update
	if flow.wake != nil {
		flow.wake()
	}
}

func (flow *CatDownloadFlow) Sync(model *appui.DownloadSelectModel) bool {
	select {
	case update := <-flow.updates:
		if update.err != nil {
			logger.Error("cat download discovery: %v", update.err)
			model.SetError(update.err.Error())
			return true
		}
		switch update.kind {
		case catDownloadUpdatePurchases:
			flow.mode, flow.keys = catDownloadModePurchases, update.keys
			choices := make([]appui.DownloadChoice, 0, len(update.keys))
			for _, key := range update.keys {
				label := "Individual purchase"
				if key.BundleSize > 1 && key.BundleName != "" {
					label = "Bundle: " + key.BundleName
				} else if key.BundleSize > 1 {
					label = fmt.Sprintf("Bundle purchase (%d games)", key.BundleSize)
				}
				choices = append(choices, appui.DownloadChoice{
					Title: label, Badge: fmt.Sprintf("%d×", key.Downloads), Detail: "Purchase source",
				})
			}
			model.SetChoices("Choose purchase source", choices)
		case catDownloadUpdateDetected:
			if update.ext == "" {
				flow.mode = catDownloadModeFormats
				flow.uploads = []roms.Upload{update.upload}
				model.SetChoices("Type not detected — choose a format", []appui.DownloadChoice{{
					Title: update.upload.Filename, Badge: "P8.PNG",
					FormatOptions: manualFormatLabels(),
				}})
				return true
			}
			upload := update.upload
			if strings.ToLower(roms.ROMExt(upload.Filename)) != update.ext {
				upload.Filename += update.ext
			}
			upload.NeedsFormat = false
			flow.plan = flow.planForUpload(upload)
		default:
			flow.setUploads(model, update.uploads)
		}
		return true
	default:
		return false
	}
}

func (flow *CatDownloadFlow) Choose(model *appui.DownloadSelectModel) {
	if model.Cursor < 0 {
		return
	}
	switch flow.mode {
	case catDownloadModePurchases:
		if model.Cursor >= len(flow.keys) {
			return
		}
		key := flow.keys[model.Cursor]
		model.SetLoading("Finding files for selected purchase")
		go func() { flow.publish(flow.fetchForKey(key)) }()
	case catDownloadModeUploads:
		if model.Cursor < len(flow.uploads) {
			flow.plan = flow.planForUpload(flow.uploads[model.Cursor])
		}
	case catDownloadModeFormats:
		if model.Cursor >= len(flow.uploads) || model.Cursor >= len(model.Choices) {
			return
		}
		upload := flow.uploads[model.Cursor]
		choice := model.Choices[model.Cursor]
		label := choice.Badge
		if label == "AUTO" {
			model.SetLoading("Detecting file type")
			go flow.detect(upload)
			return
		}
		ext := formatExtension(label)
		if strings.ToLower(roms.ROMExt(upload.Filename)) != ext {
			upload.Filename += ext
		}
		upload.NeedsFormat = false
		flow.plan = flow.planForUpload(upload)
	}
}

func (flow *CatDownloadFlow) detect(upload roms.Upload) {
	var cdnURL string
	var err error
	if upload.DownloadKeyID != "" {
		cdnURL, err = flow.client.ResolveAuthURL(flow.cfg.APIKey, upload.UploadID, upload.DownloadKeyID)
	} else {
		cdnURL, err = flow.client.ResolveFreeURL(itchio.Upload{Filename: upload.Filename, URL: upload.URL})
	}
	if err != nil {
		flow.publish(catDownloadUpdate{kind: catDownloadUpdateDetected, upload: upload, err: err})
		return
	}
	header, err := flow.client.FetchFileHeader(cdnURL, roms.DetectBufSize)
	ext := ""
	if err == nil {
		ext = roms.DetectROMExt(header)
		if ext == "" && len(header) >= 4 && header[0] == 0x89 && header[1] == 'P' && header[2] == 'N' && header[3] == 'G' {
			ext = ".p8.png"
		}
	}
	flow.publish(catDownloadUpdate{kind: catDownloadUpdateDetected, upload: upload, ext: ext, err: err})
}

func (flow *CatDownloadFlow) setUploads(model *appui.DownloadSelectModel, uploads []roms.Upload) {
	if len(uploads) == 0 {
		model.SetError("No downloadable files were found for this game.")
		return
	}
	flow.uploads = uploads
	var known, unknown []roms.Upload
	for _, upload := range uploads {
		if upload.NeedsFormat {
			unknown = append(unknown, upload)
		} else {
			known = append(known, upload)
		}
	}
	if len(known) == 1 {
		flow.plan = flow.planForUpload(known[0])
		return
	}
	if len(known) > 1 {
		hasArchive := false
		for _, upload := range known {
			hasArchive = hasArchive || isArchive(upload.Filename)
		}
		if !hasArchive {
			flow.plan = flow.planForUploads(known)
			return
		}
		flow.mode, flow.uploads = catDownloadModeUploads, known
		choices := make([]appui.DownloadChoice, 0, len(known))
		for _, upload := range known {
			choices = append(choices, appui.DownloadChoice{Title: upload.Filename, Badge: formatBadge(upload.Filename)})
		}
		model.SetChoices("Choose file to download", choices)
		return
	}
	flow.mode, flow.uploads = catDownloadModeFormats, unknown
	choices := make([]appui.DownloadChoice, 0, len(unknown))
	for _, upload := range unknown {
		choices = append(choices, appui.DownloadChoice{
			Title: upload.Filename, Badge: "AUTO", FormatOptions: append([]string(nil), allFormatLabels()...),
		})
	}
	model.SetChoices("Choose file and format", choices)
}

func (flow *CatDownloadFlow) TakePlan() *CatDownloadPlan {
	plan := flow.plan
	flow.plan = nil
	return plan
}

func (flow *CatDownloadFlow) planForUpload(upload roms.Upload) *CatDownloadPlan {
	if isArchive(upload.Filename) {
		return &CatDownloadPlan{Kind: CatDownloadPlanArchive, Uploads: []roms.Upload{upload}}
	}
	if flow.cfg.ROMLocation == "ask" {
		return &CatDownloadPlan{Kind: CatDownloadPlanDestination, Uploads: []roms.Upload{upload}}
	}
	ext := strings.ToLower(roms.ROMExt(upload.Filename))
	dest := roms.DestinationDir(ext, flow.cfg.Pico8Core) + upload.Filename
	if existing := flow.inv.ExistingDestPath(flow.game.URL, upload.Filename); existing != "" {
		dest = existing
	}
	return &CatDownloadPlan{Kind: CatDownloadPlanDirect, Uploads: []roms.Upload{upload}, DestPaths: []string{dest}}
}

func (flow *CatDownloadFlow) planForUploads(uploads []roms.Upload) *CatDownloadPlan {
	if flow.cfg.ROMLocation == "ask" {
		return &CatDownloadPlan{Kind: CatDownloadPlanDestination, Uploads: append([]roms.Upload(nil), uploads...)}
	}
	plan := &CatDownloadPlan{Kind: CatDownloadPlanMulti, Uploads: append([]roms.Upload(nil), uploads...)}
	for _, upload := range uploads {
		ext := strings.ToLower(roms.ROMExt(upload.Filename))
		dest := roms.DestinationDir(ext, flow.cfg.Pico8Core) + upload.Filename
		if existing := flow.inv.ExistingDestPath(flow.game.URL, upload.Filename); existing != "" {
			dest = existing
		}
		plan.DestPaths = append(plan.DestPaths, dest)
	}
	return plan
}

func isArchive(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".zip" || ext == ".7z"
}

func formatBadge(filename string) string {
	ext := strings.TrimPrefix(strings.ToUpper(roms.ROMExt(filename)), ".")
	if ext == "P8.PNG" {
		return ext
	}
	return ext
}

func allFormatLabels() []string {
	return []string{"AUTO", "P8.PNG", "P8", "GBC", "GB", "GBA", "NES", "MD", "ZIP"}
}

func manualFormatLabels() []string { return allFormatLabels()[1:] }

func formatExtension(label string) string {
	switch label {
	case "P8.PNG":
		return ".p8.png"
	default:
		return "." + strings.ToLower(label)
	}
}

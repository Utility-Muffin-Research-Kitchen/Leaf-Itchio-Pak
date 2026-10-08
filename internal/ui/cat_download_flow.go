//go:build !headless

package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/screentext"
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
	// LogicalExts describes the ROM type when the downloaded filename itself
	// is an archive. It lets the source-aware destination picker place a
	// single-ROM ZIP under the correct Leaf system instead of guessing from
	// the outer .zip suffix.
	LogicalExts []string
	Transaction DownloadTransaction
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
	hidden  []roms.Upload // desktop and web builds behind "Show all files"
	updates chan catDownloadUpdate
	plan    *CatDownloadPlan

	// ctx bounds every request the flow makes: discovery, purchase listings
	// and format probes. Close cancels it when you leave the download screens.
	ctx    context.Context
	cancel context.CancelFunc
}

func NewCatDownloadFlow(client *itchio.Client, cfg *settings.Config, game itchio.Game,
	detail *itchio.GameDetail, inv *inventory.Inventory, wake func()) *CatDownloadFlow {
	if detail != nil && detail.Data != nil {
		game.IsFree = detail.Data.Pricing() != itchio.PricingPaid
	}
	flow := &CatDownloadFlow{
		client: client, cfg: cfg, game: game, detail: detail, inv: inv, wake: wake,
		updates: make(chan catDownloadUpdate, 2),
	}
	flow.ctx, flow.cancel = context.WithCancel(context.Background())
	flow.discover()
	return flow
}

// Close stops the flow's requests, including any wait before a rate-limit
// retry. Call it when the download screens are left; later results are
// dropped with the flow. Plans already handed to a download keep working.
func (flow *CatDownloadFlow) Close() {
	if flow.cancel != nil {
		flow.cancel()
	}
}

func (flow *CatDownloadFlow) requestContext() context.Context {
	if flow.ctx == nil {
		return context.Background()
	}
	return flow.ctx
}

func (flow *CatDownloadFlow) discover() {
	go func() {
		update := catDownloadUpdate{}
		if !flow.game.IsFree && flow.cfg.SignedIn() && flow.detail != nil && flow.detail.GameID != "" {
			keys, err := flow.client.FetchOwnedKeysContext(flow.requestContext(), flow.cfg.Credential(), flow.detail.GameID)
			if err != nil {
				update.err = err
			} else if len(keys) > 1 {
				update.kind = catDownloadUpdatePurchases
				update.keys = itchio.AnnotateBundleNames(keys, flow.detail.BundleNames)
			} else if len(keys) == 1 {
				update = flow.fetchForKey(keys[0])
			} else {
				update.err = itchio.ErrNotOwned
			}
		} else if flow.game.IsFree && flow.cfg.SignedIn() && flow.detail != nil && flow.detail.GameID != "" {
			update = flow.fetchFree()
		} else {
			update = flow.fetchWeb()
		}
		flow.publish(update)
	}()
}

// fetchWeb lists uploads through the anonymous web download flow.
func (flow *CatDownloadFlow) fetchWeb() catDownloadUpdate {
	uploads, err := flow.client.FetchWebUploadsContext(flow.requestContext(), flow.game.URL)
	update := catDownloadUpdate{kind: catDownloadUpdateUploads, err: err}
	update.uploads = flow.dropTextMarkdown(listedUploads(uploads, webUploadListing(uploads), nil))
	return update
}

// listedUploads turns one upload list into the uploads you choose from. It
// is the one place that gives each upload the list it was chosen from: the
// install uses that list to tell an update, whose old upload the list no
// longer offers, from another build it still offers, and seeds update
// checks with it. install is nil for the web flow.
func listedUploads(uploads []itchio.Upload, listing *roms.UploadListing, install *roms.InstallSession) []roms.Upload {
	var listed []roms.Upload
	for _, upload := range uploads {
		listed = append(listed, roms.Upload{
			Filename: upload.Filename, URL: upload.URL, UploadID: upload.UploadID,
			UploadFingerprint: upload.Fingerprint(), NeedsFormat: upload.NeedsFormat,
			DesktopOrWeb: upload.DesktopOrWebOnly(), Install: install, Listing: listing,
		})
	}
	return listed
}

// apiUploadListing and webUploadListing record what a listing offered, so
// the install chosen from it can seed update checks. Web builds are left
// out, as the update check leaves them out.
func apiUploadListing(uploads []itchio.Upload) *roms.UploadListing {
	return uploadListing(true, uploads)
}

func webUploadListing(uploads []itchio.Upload) *roms.UploadListing {
	return uploadListing(false, uploads)
}

func uploadListing(api bool, uploads []itchio.Upload) *roms.UploadListing {
	listing := &roms.UploadListing{API: api, Uploads: make([]roms.ListedUpload, 0, len(uploads))}
	for _, upload := range uploads {
		if upload.Type == "html" {
			continue
		}
		listing.Uploads = append(listing.Uploads, roms.ListedUpload{
			Filename: upload.Filename, DisplayName: upload.DisplayName,
			UploadID: upload.UploadID, Fingerprint: upload.Fingerprint(),
			DesktopOrWebOnly: upload.DesktopOrWebOnly(), Soundtrack: upload.Type == "soundtrack",
		})
	}
	return listing
}

// fetchFree lists a free or name-your-own-price game through the API when a
// key is set, which skips the web download handshake and its download_url
// POST. The listing starts one install with no purchase ID.
//
// It falls back to the web flow at most once, when the API fails or lists
// nothing, so the two endpoints are never tried in a loop. A rate limit or
// cancellation is final: trying the other endpoint would ignore it. When the
// API refused access and the web flow found no download link either, the
// access error is the one reported; any other web failure, such as being
// offline, is reported as is.
func (flow *CatDownloadFlow) fetchFree() catDownloadUpdate {
	uploads, err := flow.client.FetchUploadsContext(flow.requestContext(), flow.cfg.Credential(), flow.detail.GameID, "")
	switch {
	case err == nil && len(uploads) > 0:
		logger.Info("cat download: free game_id=%s listed through the API (%d upload(s))", flow.detail.GameID, len(uploads))
		install := roms.NewInstallSession(flow.detail.GameID, "")
		return catDownloadUpdate{kind: catDownloadUpdateUploads,
			uploads: flow.dropTextMarkdown(listedUploads(uploads, apiUploadListing(uploads), install))}
	case errors.Is(err, itchio.ErrRateLimited) || errors.Is(err, context.Canceled):
		return catDownloadUpdate{kind: catDownloadUpdateUploads, err: err}
	case err != nil:
		logger.Warn("cat download: free game API listing failed, using the web flow: %v", err)
	default:
		logger.Info("cat download: API lists no uploads for free game_id=%s, using the web flow", flow.detail.GameID)
	}
	update := flow.fetchWeb()
	if errors.Is(update.err, itchio.ErrNoWebDownload) && errors.Is(err, itchio.ErrNoAccess) {
		update.err = err
	}
	return update
}

// fetchForKey lists the uploads a purchase grants. Each listing starts a new
// install: its uploads share one session for probes, inspection, refreshed
// URLs, and every file downloaded.
func (flow *CatDownloadFlow) fetchForKey(key itchio.OwnedKey) catDownloadUpdate {
	downloadKeyID := strconv.FormatInt(key.ID, 10)
	uploads, err := flow.client.FetchUploadsContext(flow.requestContext(), flow.cfg.Credential(), flow.detail.GameID, downloadKeyID)
	update := catDownloadUpdate{kind: catDownloadUpdateUploads, err: err}
	install := roms.NewInstallSession(flow.detail.GameID, downloadKeyID)
	var listing *roms.UploadListing
	if err == nil {
		listing = apiUploadListing(uploads)
	}
	update.uploads = flow.dropTextMarkdown(listedUploads(uploads, listing, install))
	return update
}

// mdProbeBytes is how much of a ".md" upload is read to tell a Mega Drive
// ROM from Markdown; roms.MDIsROM checks the first 4 KB for text.
const mdProbeBytes = 4096

// dropTextMarkdown removes ".md" uploads that are text, such as a README
// published next to the game. ".md" is also the Mega Drive extension, so
// each one is checked by its first bytes with the same rule as archive
// members. An upload that cannot be checked stays offered.
func (flow *CatDownloadFlow) dropTextMarkdown(uploads []roms.Upload) []roms.Upload {
	kept := make([]roms.Upload, 0, len(uploads))
	for _, upload := range uploads {
		if strings.EqualFold(roms.ROMExt(upload.Filename), ".md") && flow.uploadIsText(upload) {
			logger.Info("download: not offering %s; it is text, not a Mega Drive ROM", upload.Filename)
			continue
		}
		kept = append(kept, upload)
	}
	return kept
}

func (flow *CatDownloadFlow) uploadIsText(upload roms.Upload) bool {
	cdnURL, err := resolveUploadURL(flow.requestContext(), flow.client, flow.cfg.Credential(), flow.game.URL, upload)
	if err == nil {
		var header []byte
		if header, err = flow.client.FetchFileHeader(cdnURL, mdProbeBytes); err == nil {
			return !roms.MDIsROM(header)
		}
	}
	logger.Warn("download: could not check %s for Markdown, offering it: %v", upload.Filename, err)
	return false
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
			model.SetError(screentext.FromError(update.err))
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
				if roms.IsPSXSupportExt(roms.ROMExt(update.upload.Filename)) {
					model.SetError("This standalone BIN is not a recognized cartridge ROM. A PlayStation BIN track requires its matching CUE descriptor.")
					return true
				}
				flow.mode = catDownloadModeFormats
				flow.uploads = []roms.Upload{update.upload}
				model.SetChoices("Type not detected. Choose a format", []appui.DownloadChoice{{
					Title: update.upload.Filename, Badge: "P8.PNG",
					FormatOptions: manualFormatLabels(),
				}})
				return true
			}
			upload := update.upload
			upload.Filename = filenameWithFormat(upload.Filename, update.ext)
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
		} else if len(flow.hidden) > 0 {
			// "Show all files": list the set-aside builds last and move
			// to the first of them.
			first := len(flow.uploads)
			flow.chooseUpload(model, append(append([]roms.Upload(nil), flow.uploads...), flow.hidden...), nil)
			model.Cursor = first
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
		upload.Filename = filenameWithFormat(upload.Filename, ext)
		upload.NeedsFormat = false
		flow.plan = flow.planForUpload(upload)
	}
}

func (flow *CatDownloadFlow) detect(upload roms.Upload) {
	cdnURL, err := resolveUploadURL(flow.requestContext(), flow.client, flow.cfg.Credential(), flow.game.URL, upload)
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
	flow.uploads, flow.hidden = uploads, nil
	if len(uploads) == 1 && roms.IsPSXSupportExt(roms.ROMExt(uploads[0].Filename)) {
		// BIN is ambiguous: it is commonly a PlayStation companion track, but
		// Mega Drive homebrew is also frequently published as a lone .bin.
		// Require content detection before applying the orphan-PSX guard.
		upload := uploads[0]
		upload.NeedsFormat = true
		flow.mode, flow.uploads = catDownloadModeFormats, []roms.Upload{upload}
		model.SetChoices("Detect standalone BIN format", []appui.DownloadChoice{{
			Title: upload.Filename, Badge: "AUTO", FormatOptions: append([]string(nil), allFormatLabels()...),
		}})
		return
	}
	var known, unknown []roms.Upload
	for _, upload := range uploads {
		if upload.NeedsFormat {
			unknown = append(unknown, upload)
		} else {
			known = append(known, upload)
		}
	}
	// Desktop and web builds are never picked automatically. While another
	// file is on offer they wait behind "Show all files"; when only they
	// remain, you choose.
	known, setAside := splitSetAside(known)
	unknown = setAsideLast(unknown)
	if len(setAside) > 0 {
		logger.Debug("cat download: %d desktop or web build(s) set aside", len(setAside))
	}
	if len(known) == 0 && len(setAside) > 0 {
		flow.chooseUpload(model, setAside, nil)
		return
	}
	if flow.cfg.ROMSelection == "ask" && len(known) > 0 && !isPairedPSXUploadSet(known) {
		flow.chooseUpload(model, known, setAside)
		return
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
		if !hasArchive && !hasAlternativeBuilds(known) {
			flow.plan = flow.planForUploads(known)
			return
		}
		flow.chooseUpload(model, known, setAside)
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

// chooseUpload lists uploads for you to pick from. hidden files wait behind
// a last "Show all files" row.
func (flow *CatDownloadFlow) chooseUpload(model *appui.DownloadSelectModel, uploads, hidden []roms.Upload) {
	flow.mode, flow.uploads, flow.hidden = catDownloadModeUploads, uploads, hidden
	choices := make([]appui.DownloadChoice, 0, len(uploads)+1)
	for _, upload := range uploads {
		choices = append(choices, appui.DownloadChoice{Title: upload.Filename, Badge: formatBadge(upload.Filename)})
	}
	if len(hidden) > 0 {
		choices = append(choices, appui.DownloadChoice{Title: "Show all files", Detail: fmt.Sprintf("%d more", len(hidden))})
	}
	model.SetChoices("Choose file to download", choices)
}

// setAside reports whether upload is a desktop or web build that is not a
// ROM itself. A file with a ROM extension is kept even when its author
// tagged it with a platform.
func setAside(upload roms.Upload) bool {
	return upload.DesktopOrWeb && (upload.NeedsFormat || isArchive(upload.Filename))
}

// splitSetAside separates set-aside builds from the other uploads, keeping
// the listing order of each.
func splitSetAside(uploads []roms.Upload) (kept, aside []roms.Upload) {
	for _, upload := range uploads {
		if setAside(upload) {
			aside = append(aside, upload)
		} else {
			kept = append(kept, upload)
		}
	}
	return kept, aside
}

// setAsideLast moves set-aside builds to the end, keeping the listing order.
func setAsideLast(uploads []roms.Upload) []roms.Upload {
	kept, aside := splitSetAside(uploads)
	return append(kept, aside...)
}

// hasAlternativeBuilds reports whether two uploads target the same cartridge
// system, which makes them alternative builds of one game (an update and the
// original jam release, say) rather than companions for different systems.
// PlayStation files are left out: CUE/BIN tracks and the discs of one game
// are a dependent set, not competing builds.
func hasAlternativeBuilds(uploads []roms.Upload) bool {
	seen := make(map[string]bool, len(uploads))
	for _, upload := range uploads {
		ext := strings.ToLower(roms.ROMExt(upload.Filename))
		if roms.IsPSXExt(ext) {
			continue
		}
		system := roms.DestinationDir(ext)
		if system == "" {
			continue
		}
		if seen[system] {
			return true
		}
		seen[system] = true
	}
	return false
}

func isPairedPSXUploadSet(uploads []roms.Upload) bool {
	hasCUE, hasBIN := false, false
	for _, upload := range uploads {
		switch strings.ToLower(roms.ROMExt(upload.Filename)) {
		case ".cue":
			hasCUE = true
		case ".bin":
			hasBIN = true
		}
	}
	return hasCUE && hasBIN
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
	dest := roms.DestinationDir(ext) + upload.Filename
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
		dest := roms.DestinationDir(ext) + upload.Filename
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
	return []string{"AUTO", "P8.PNG", "P8", "GBC", "GB", "GBA", "NES", "MD",
		"CHD", "PBP", "CUE", "ISO", "IMG", "MDF", "TOC", "CBN", "M3U", "ZIP"}
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

func filenameWithFormat(filename, ext string) string {
	current := roms.ROMExt(filename)
	if strings.EqualFold(current, ext) {
		return filename
	}
	if strings.EqualFold(current, ".bin") {
		return strings.TrimSuffix(filename, current) + ext
	}
	return filename + ext
}

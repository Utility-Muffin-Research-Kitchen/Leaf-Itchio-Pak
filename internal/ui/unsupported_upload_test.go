//go:build !headless

package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// webGameSite serves a free game's anonymous download flow with the given
// upload names, numbered from 1.
func webGameSite(t *testing.T, names ...string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/game":
			fmt.Fprint(w, `<html><head><meta name="csrf_token" value="CSRF"/></head></html>`)
		case "/game/download_url":
			json.NewEncoder(w).Encode(map[string]string{"url": srv.URL + "/dl/KEY"})
		case "/dl/KEY":
			var page strings.Builder
			page.WriteString(`<html><head><meta name="csrf_token" value="DL"/></head><body>`)
			for i, name := range names {
				fmt.Fprintf(&page, `<div class="upload"><div class="info_column"><div class="upload_name">`+
					`<strong class="name" title=%q>%s</strong></div></div><div class="actions">`+
					`<a class="button download_btn" href="javascript:void(0);" data-upload_id="%d">Download</a>`+
					`</div></div>`, name, name, i+1)
			}
			page.WriteString(`</body></html>`)
			fmt.Fprint(w, page.String())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// discoverWeb runs the anonymous discovery of a free game against srv and
// applies its answer to a fresh picker.
func discoverWeb(t *testing.T, srv *httptest.Server, platform string) (*CatDownloadFlow, *appui.DownloadSelectModel) {
	t.Helper()
	flow, model := newCatDownloadFlowForTest(t)
	flow.client = itchio.NewClientWithBase(srv.URL)
	flow.cfg = &settings.Config{ROMLocation: "auto"}
	flow.game = itchio.Game{Title: "Hidden palace", URL: srv.URL + "/game", IsFree: true, Platform: platform}
	flow.updates = make(chan catDownloadUpdate, 1)
	flow.discover()
	waitFor(t, func() bool { return flow.Sync(model) })
	return flow, model
}

// F31: Hidden palace is listed under PlayStation, but its uploads are
// "Hidden_palace.nds v0.1 (Post-jam bug fix)" and a jam version. The picker
// no longer offers them as ROMs to classify; it says what they are.
func TestNDSUploadsAreReportedUnsupportedAndNotOffered(t *testing.T) {
	srv := webGameSite(t, "Hidden_palace.nds v0.1 (Post-jam bug fix)", "Hidden_palace.nds v0.0 (Jam version)")
	flow, model := discoverWeb(t, srv, "PSX")
	if model.State != appui.DownloadSelectError {
		t.Fatalf("state = %v with choices %q, want an error that says the files cannot be installed", model.State, choiceTitles(model))
	}
	const want = "This game has no files the app can install. Its files are for Nintendo DS."
	if model.Message != want {
		t.Fatalf("message = %q, want %q", model.Message, want)
	}
	if flow.TakePlan() != nil || len(model.Choices) != 0 {
		t.Fatalf("plan %#v, choices %q; want neither", flow.TakePlan(), choiceTitles(model))
	}
}

func formatOptionsOf(model *appui.DownloadSelectModel) []string {
	if len(model.Choices) == 0 {
		return nil
	}
	return model.Choices[0].FormatOptions
}

// F31: when the type is truly unknown, the formats of the system the game is
// listed under come first, in place of P8.PNG. AUTO, the detection, stays
// first.
func TestUnknownUploadOffersTheListedSystemsFormatsFirst(t *testing.T) {
	for _, tc := range []struct {
		platform string
		want     []string // the first formats after AUTO
	}{
		{"PSX", []string{"CHD", "PBP", "CUE", "ISO", "IMG", "MDF", "TOC", "CBN", "M3U", "P8.PNG"}},
		{"GBA", []string{"GBA", "P8.PNG"}},
		{"GBC", []string{"GBC", "P8.PNG"}},
		{"GB", []string{"GB", "P8.PNG"}},
		{"NES", []string{"NES", "P8.PNG"}},
		{"MD", []string{"MD", "P8.PNG"}},
		{"P8", []string{"P8.PNG", "P8", "GBC"}},
		{"", []string{"P8.PNG", "P8", "GBC"}},
		{"NDS", []string{"P8.PNG", "P8", "GBC"}},
	} {
		t.Run("choose file and format, "+tc.platform, func(t *testing.T) {
			flow, model := newCatDownloadFlowForTest(t)
			flow.game.Platform = tc.platform
			flow.setUploads(model, []roms.Upload{{Filename: "mystery", NeedsFormat: true}})
			options := formatOptionsOf(model)
			if len(options) != len(allFormatLabels()) || options[0] != "AUTO" || model.Choices[0].Badge != "AUTO" {
				t.Fatalf("format options = %q, badge %q; want every format, AUTO first", options, model.Choices[0].Badge)
			}
			if got := options[1 : 1+len(tc.want)]; strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("formats after AUTO = %q, want them to start %q", options[1:], tc.want)
			}
		})
		t.Run("type not detected, "+tc.platform, func(t *testing.T) {
			flow, model := newCatDownloadFlowForTest(t)
			flow.game.Platform = tc.platform
			flow.updates = make(chan catDownloadUpdate, 1)
			flow.publish(catDownloadUpdate{kind: catDownloadUpdateDetected,
				upload: roms.Upload{Filename: "mystery", NeedsFormat: true}})
			if !flow.Sync(model) || model.State != appui.DownloadSelectChoices || len(model.Choices) != 1 {
				t.Fatalf("model = %#v, want the manual format picker", model)
			}
			choice := model.Choices[0]
			if got := choice.FormatOptions[:len(tc.want)]; strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("format options = %q, want them to start %q", choice.FormatOptions, tc.want)
			}
			if choice.Badge != tc.want[0] || len(choice.FormatOptions) != len(allFormatLabels())-1 {
				t.Fatalf("badge %q of %d formats; want %q first of every format but AUTO", choice.Badge, len(choice.FormatOptions), tc.want[0])
			}
		})
	}
}

// F31: choosing the preselected format of a PlayStation game installs a
// PlayStation file, not a Pico-8 cart.
func TestUnknownUploadOfAPlayStationGameDefaultsToAPlayStationFormat(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.game.Platform = "PSX"
	flow.updates = make(chan catDownloadUpdate, 1)
	flow.publish(catDownloadUpdate{kind: catDownloadUpdateDetected, upload: roms.Upload{Filename: "mystery", NeedsFormat: true}})
	flow.Sync(model)
	flow.Choose(model)
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || plan.Uploads[0].Filename != "mystery.chd" {
		t.Fatalf("plan = %#v, want mystery.chd", plan)
	}
}

// F31: the same holds for a listing through the API. A Nintendo DS build next
// to a Game Boy ROM is not offered, and the ROM installs without asking.
func TestAPIListedNDSUploadIsNotOffered(t *testing.T) {
	site := newFreeGameSite(t, apiUploads(`{"uploads":[
		{"id":5,"filename":"Hidden_palace.nds v0.1 (Post-jam bug fix)","traits":{}},
		{"id":6,"filename":"hidden.gb","traits":{}}]}`))
	for _, selection := range []string{"auto", "ask"} {
		flow, model := newCatDownloadFlowForTest(t)
		flow.client = itchio.NewClientWithBase(site.srv.URL)
		flow.cfg.AuthToken, flow.cfg.ROMSelection = sessionTestKey, selection
		flow.game = itchio.Game{Title: "Hidden palace", URL: site.srv.URL + "/game", IsFree: true, Platform: "PSX"}
		flow.detail = &itchio.GameDetail{GameID: "42"}
		flow.updates = make(chan catDownloadUpdate, 1)
		flow.discover()
		waitFor(t, func() bool { return flow.Sync(model) })
		if selection == "auto" {
			plan := flow.TakePlan()
			if plan == nil || plan.Kind != CatDownloadPlanDirect || plan.Uploads[0].Filename != "hidden.gb" {
				t.Fatalf("auto: plan = %#v, model = %#v; want hidden.gb alone", plan, model)
			}
			continue
		}
		if titles := choiceTitles(model); len(titles) != 1 || titles[0] != "hidden.gb" {
			t.Fatalf("ask: choices = %q, want hidden.gb alone", titles)
		}
	}
}

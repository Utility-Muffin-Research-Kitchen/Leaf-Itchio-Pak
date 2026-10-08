package itchio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGameDataPricing(t *testing.T) {
	for _, tc := range []struct {
		price string
		want  PricingModel
	}{
		{"", PricingFree}, {"$0.00", PricingNameYourOwnPrice}, {"€0,00", PricingNameYourOwnPrice},
		{"¥0", PricingNameYourOwnPrice}, {"$0.01", PricingPaid}, {"€2,50", PricingPaid}, {"unknown", PricingPaid},
	} {
		if got := (&GameData{Price: tc.price}).Pricing(); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.price, got, tc.want)
		}
	}
}

func TestGameDataRedirectAndDetailFields(t *testing.T) {
	const page = `<meta content="games/1" name="itch:path"><meta name="csrf_token" value="csrf">
<div class="formatted_description"><p>Keep this description.</p></div>
<div class="bundle_title"><a href="/bundle">Keep this bundle</a></div>
<div class="buy_row">Buy</div>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("public metadata received a credential")
		}
		switch r.URL.Path {
		case "/old/data.json":
			http.Redirect(w, r, "/new/data.json", http.StatusFound)
		case "/new/data.json":
			if r.Header.Get("Accept") != "application/json" {
				t.Error("missing JSON Accept")
			}
			fmt.Fprint(w, `{"id":42,"price":"€2,50","original_price":"€5,00","sale":{"rate":50},"suggested_price":"€4,00","tags":["horror"],"screenshots":["https://img.itch.zone/shot.png"],"links":{"self":"https://author.itch.io/new"}}`)
		default:
			fmt.Fprint(w, page)
		}
	}))
	defer srv.Close()
	client := NewClient()
	detail, err := client.FetchGameDetail(srv.URL + "/old")
	if err != nil {
		t.Fatal(err)
	}
	if detail.GameID != "42" || detail.Data == nil ||
		detail.Data.Price != "€2,50" || detail.Data.OriginalPrice != "€5,00" || detail.Data.Sale.Rate != 50 {
		t.Fatalf("metadata = %#v", detail)
	}
	if detail.CSRFToken != "csrf" || !strings.Contains(detail.Description, "Keep this description") ||
		len(detail.BundleNames) != 1 || len(detail.PageTags) != 1 || len(detail.ScreenshotURLs) != 1 {
		t.Fatalf("page and metadata fields = %#v", detail)
	}
}

func TestGameDataBadResponsesStaySafeAndDetailFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		removed    bool
	}{
		{"missing", "", 404, true}, {"gone", "", 410, true}, {"outage", "private-server-text", 503, false},
		{"truncated", `{"id":42,"secret":"private-server-text"`, 200, false},
		{"oversized", `{"id":42,"secret":"` + strings.Repeat("x", dataJSONMaxBytes) + `"}`, 200, false},
		{"missing identity", `{}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/data.json") {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
					return
				}
				fmt.Fprint(w, `<meta name="itch:path" content="games/7"><div class="formatted_description"><p>Fallback</p></div>`)
			}))
			defer srv.Close()
			client := NewClient()
			_, err := client.FetchGameData(srv.URL)
			if err == nil || errors.Is(err, ErrGameRemoved) != tc.removed || strings.Contains(err.Error(), "private-server-text") || strings.Contains(err.Error(), srv.URL) {
				t.Fatalf("unsafe/wrong error: %v", err)
			}
			detail, err := client.FetchGameDetail(srv.URL)
			if err != nil || detail.Data != nil || detail.GameID != "7" || !strings.Contains(detail.Description, "Fallback") {
				t.Fatalf("fallback: detail=%#v err=%v", detail, err)
			}
		})
	}
}

func TestGameDataCancellationAndBrowserOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/data.json") {
			fmt.Fprint(w, `{"id":42,"price":"$0.00"}`)
			return
		}
		fmt.Fprint(w, `<div class="html_embed_widget">html.itch.zone</div>`)
	}))
	defer srv.Close()
	client := NewClient()
	detail, err := client.FetchGameDetail(srv.URL)
	if err != nil || !detail.BrowserOnly {
		t.Fatalf("browser-only: %#v %v", detail, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.FetchGameDataContext(ctx, srv.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestDetailTagsAreTheUnionOfPageAndMetadataTags(t *testing.T) {
	// Content warnings read these tags, so neither source may drop one.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/data.json") {
			fmt.Fprint(w, `{"id":42,"tags":["Horror","Puzzle"]}`)
			return
		}
		fmt.Fprint(w, `<a href="https://itch.io/games/tag-gore">Gore</a><a href="https://itch.io/games/tag-horror">Horror</a>`)
	}))
	defer srv.Close()
	detail, err := NewClient().FetchGameDetail(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(detail.PageTags, ","); got != "gore,horror,Puzzle" {
		t.Fatalf("tags = %q, want gore,horror,Puzzle", got)
	}
}

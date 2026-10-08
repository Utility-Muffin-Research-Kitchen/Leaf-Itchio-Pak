//go:build !headless

package ui

import (
	"fmt"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/screentext"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type catDetailResult struct {
	detail *itchio.GameDetail
	err    error
}

type CatDetailLoader struct {
	game    itchio.Game
	updates chan catDetailResult
	detail  *itchio.GameDetail
}

func NewCatDetailLoader(client *itchio.Client, cfg *settings.Config, game itchio.Game, wake func()) *CatDetailLoader {
	loader := &CatDetailLoader{game: game, updates: make(chan catDetailResult, 1)}
	go func() {
		result := catDetailResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				result.err = fmt.Errorf("internal error: %v", recovered)
			}
			loader.updates <- result
			if wake != nil {
				wake()
			}
		}()
		result.detail, result.err = client.FetchGameDetail(game.URL)
	}()
	return loader
}

func (loader *CatDetailLoader) Sync(model *appui.DetailModel, cfg *settings.Config) bool {
	select {
	case result := <-loader.updates:
		if result.err != nil {
			logger.Error("cat detail: %v", result.err)
			model.Tags = dedupeStrings(loader.game.Tags)
			model.Images = dedupeStrings([]string{loader.game.CoverURL})
			model.SetError(unavailableDetail(result.err))
			if itchio.IsAdvisoryTriggered(model.Tags, catFilterConfig(cfg)) {
				model.State = appui.DetailWarning
			}
			return true
		}
		detail := result.detail
		loader.detail = detail
		if data := detail.Data; data != nil {
			// The current price decides IsFree. The page's action follows
			// from it and the account when CatalogController.ApplyDetailAccess
			// runs before each draw.
			loader.game.IsFree = data.Pricing() != itchio.PricingPaid
			model.Game.IsFree = loader.game.IsFree
			if data.CoverImage != "" {
				loader.game.CoverURL = data.CoverImage
			}
			model.Game.PriceLabel = detailPriceLabel(data)
		}
		images := dedupeStrings(append([]string{loader.game.CoverURL}, detail.ScreenshotURLs...))
		tags := dedupeStrings(append(append([]string{}, loader.game.Tags...), detail.PageTags...))
		// Catalogue and page tags together, so a tag that warns on the
		// unavailable page also warns here.
		warning := itchio.IsAdvisoryTriggered(tags, catFilterConfig(cfg))
		model.SetReady(detail.Description, tags, images, detail.BrowserOnly, warning)
		return true
	default:
		return false
	}
}

// unavailableDetail says why the game page could not load and what to do.
// Reopening never brings back a removed game, and retrying at once is what
// the rate limiter exists to prevent. Here, trying again means reopening
// the page.
func unavailableDetail(err error) string {
	switch screentext.Classify(err) {
	case screentext.GameRemoved:
		return screentext.FromError(err)
	case screentext.RateLimited:
		return "itch.io is limiting requests. Wait a minute, then reopen this game."
	case screentext.Network:
		return "Can't reach itch.io. Check the connection, then reopen this game."
	default:
		return "Go back and reopen this game to try again."
	}
}

// detailPriceLabel is the price shown on the detail page. Labels stay short:
// the subtitle line also carries the author and platform. Ownership is not
// part of it: DetailGame.PriceText shows "Owned" from the current account.
func detailPriceLabel(data *itchio.GameData) string {
	onSale := data.OriginalPrice != "" && data.OriginalPrice != data.Price
	switch data.Pricing() {
	case itchio.PricingPaid:
		switch {
		case onSale:
			return data.Price + " (was " + data.OriginalPrice + ")"
		case data.SuggestedPrice != "":
			// A suggestion above a non-zero price means you pay at least it.
			return data.Price + " or more"
		}
		return data.Price
	case itchio.PricingNameYourOwnPrice:
		switch {
		case onSale:
			return "Free (was " + data.OriginalPrice + ")"
		case data.SuggestedPrice != "":
			return "Free / suggested " + data.SuggestedPrice
		}
		return "Free / name your price"
	}
	if onSale {
		return "Free (was " + data.OriginalPrice + ")"
	}
	return "Free"
}

// Detail returns the fully scraped detail after Sync publishes a ready model.
// It is only read and written by the Cat owner thread.
func (loader *CatDetailLoader) Detail() *itchio.GameDetail { return loader.detail }

// Game preserves inventory identity while refreshing mutable public fields.
func (loader *CatDetailLoader) Game() itchio.Game { return loader.game }

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func catFilterConfig(cfg *settings.Config) itchio.FilterConfig {
	if cfg == nil {
		return itchio.FilterConfig{}
	}
	convert := func(value settings.CategoryFilter) itchio.CategoryFilter {
		return itchio.CategoryFilter{Enabled: value.Enabled, Disabled: append([]string(nil), value.Disabled...)}
	}
	return itchio.FilterConfig{
		AdultContent: convert(cfg.Filter.AdultContent), QueerContent: convert(cfg.Filter.QueerContent),
		HeavyThemes: convert(cfg.Filter.HeavyThemes), SubstanceUse: convert(cfg.Filter.SubstanceUse),
	}
}

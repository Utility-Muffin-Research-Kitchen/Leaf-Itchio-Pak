//go:build !headless

package ui

import (
	"fmt"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type catDetailResult struct {
	detail *itchio.GameDetail
	err    error
}

type CatDetailLoader struct {
	game    itchio.Game
	updates chan catDetailResult
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
			model.SetError(result.err.Error())
			return true
		}
		detail := result.detail
		images := dedupeStrings(append([]string{loader.game.CoverURL}, detail.ScreenshotURLs...))
		tags := dedupeStrings(append(append([]string{}, loader.game.Tags...), detail.PageTags...))
		warning := itchio.IsAdvisoryTriggered(detail.PageTags, catFilterConfig(cfg))
		model.SetReady(detail.Description, tags, images, detail.BrowserOnly, warning)
		return true
	default:
		return false
	}
}

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

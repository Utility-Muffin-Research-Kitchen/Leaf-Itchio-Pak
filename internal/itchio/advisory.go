package itchio

import (
	"slices"
	"strings"
)

// CategoryFilter holds the enabled state and individually-disabled tags for
// one content filter category. Disabled is an opt-out list: tags in this list
// are excluded from filtering even when Enabled is true.
type CategoryFilter struct {
	Enabled  bool
	Disabled []string
}

// FilterConfig is the complete content filter configuration passed to
// IsAdvisoryTriggered. It lives in the itchio package (not settings) to avoid
// import cycles — callers in the ui package convert from settings.ContentFilter.
type FilterConfig struct {
	AdultContent CategoryFilter
	QueerContent CategoryFilter
	HeavyThemes  CategoryFilter
	SubstanceUse CategoryFilter
}

// AdultContentTags covers explicit adult content and suggestive content
// (previously split across MatureTags and SexualContentTags).
// Users can toggle the category and opt individual tags in or out.
// Sorted alphabetically.
var AdultContentTags = []string{
	"adult", "boobs", "ecchi", "eroge", "erotic", "femdom", "gore",
	"hentai", "innuendo", "lewd", "nsfw", "nudity", "porn",
	"sexual-content", "sexy", "softcore", "suggestive", "tits", "titties",
	"xxx", "yaoi", "yuri",
}

// QueerContentTags covers LGBTQ+ themes and representation.
// Users can toggle the category and opt individual tags in or out.
// Sorted alphabetically.
var QueerContentTags = []string{
	"achillean", "aromantic", "asexual", "bisexual", "enby",
	"gay", "gender", "intersex", "lesbian", "lgbt", "lgbtq",
	"lgbtqia", "mlm", "non-binary", "nonbinary", "pansexual",
	"pride", "queer", "sapphic", "trans", "transgender", "wlw",
}

// HeavyThemesTags covers potentially distressing narrative themes.
// Users can toggle the category and opt individual tags in or out.
// Sorted alphabetically.
var HeavyThemesTags = []string{
	"abuse", "anxiety", "bereavement", "child-loss", "death",
	"depression", "domestic-abuse", "eating-disorder", "grief",
	"loss", "mental-health", "mental-illness", "miscarriage",
	"self-harm", "sexual-assault", "suicide", "trauma", "war",
}

// SubstanceUseTags covers drug and alcohol themes.
var SubstanceUseTags = []string{
	"addiction", "alcohol", "drug-use", "drugs", "substance-abuse",
}

// normalizeTagList returns a copy of list with each element lowercased and trimmed.
func normalizeTagList(list []string) []string {
	out := make([]string, len(list))
	for i, d := range list {
		out[i] = strings.ToLower(strings.TrimSpace(d))
	}
	return out
}

// The categories' names, as the Content Moderation screen shows them. The
// content warning names a matched category the same way.
const (
	CategoryAdultContent = "Adult Content"
	CategoryQueerContent = "Queer Content"
	CategoryHeavyThemes  = "Heavy Themes"
	CategorySubstanceUse = "Substance Use"
)

// IsAdvisoryTriggered returns true if any tag in pageTags matches an active
// filter in cfg. Tag matching is case-insensitive and whitespace-trimmed.
func IsAdvisoryTriggered(pageTags []string, cfg FilterConfig) bool {
	return len(MatchedCategories(pageTags, cfg)) > 0
}

// MatchedCategories names the categories whose active filters match a tag in
// pageTags, each once, in the order the Content Moderation screen lists them.
// It returns nil when nothing matches. Tag matching is case-insensitive and
// whitespace-trimmed; a tag you allowed one by one does not match.
func MatchedCategories(pageTags []string, cfg FilterConfig) []string {
	var matched []string
	for _, category := range []struct {
		name   string
		tags   []string
		filter CategoryFilter
	}{
		{CategoryAdultContent, AdultContentTags, cfg.AdultContent},
		{CategoryQueerContent, QueerContentTags, cfg.QueerContent},
		{CategoryHeavyThemes, HeavyThemesTags, cfg.HeavyThemes},
		{CategorySubstanceUse, SubstanceUseTags, cfg.SubstanceUse},
	} {
		if !category.filter.Enabled {
			continue
		}
		// Normalise the opt-out list once, outside the per-tag loop.
		allowed := normalizeTagList(category.filter.Disabled)
		for _, tag := range pageTags {
			slug := strings.ToLower(strings.TrimSpace(tag))
			if slices.Contains(category.tags, slug) && !slices.Contains(allowed, slug) {
				matched = append(matched, category.name)
				break
			}
		}
	}
	return matched
}

package itchio_test

import (
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

func cfg(adult, queer, heavy, substance itchio.CategoryFilter) itchio.FilterConfig {
	return itchio.FilterConfig{
		AdultContent: adult,
		QueerContent: queer,
		HeavyThemes:  heavy,
		SubstanceUse: substance,
	}
}

var off = itchio.CategoryFilter{}

// ── Adult Content ─────────────────────────────────────────────────────────────

func TestAdultContentMatchExplicit(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if !itchio.IsAdvisoryTriggered([]string{"nsfw"}, cfg(on, off, off, off)) {
		t.Error("expected trigger for explicit adult tag 'nsfw'")
	}
}

func TestAdultContentMatchSuggestive(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if !itchio.IsAdvisoryTriggered([]string{"suggestive"}, cfg(on, off, off, off)) {
		t.Error("expected trigger for suggestive tag 'suggestive'")
	}
}

func TestAdultContentCaseInsensitive(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if !itchio.IsAdvisoryTriggered([]string{"NSFW"}, cfg(on, off, off, off)) {
		t.Error("expected trigger for uppercase 'NSFW'")
	}
}

func TestAdultContentDisabled(t *testing.T) {
	if itchio.IsAdvisoryTriggered([]string{"nsfw"}, cfg(off, off, off, off)) {
		t.Error("expected no trigger when adult content filter disabled")
	}
}

func TestAdultContentWhitespaceTrimmed(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if !itchio.IsAdvisoryTriggered([]string{" nsfw "}, cfg(on, off, off, off)) {
		t.Error("expected trigger for tag with surrounding whitespace")
	}
}

func TestAdultContentTagIndividuallyDisabled(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true, Disabled: []string{"nsfw"}}
	if itchio.IsAdvisoryTriggered([]string{"nsfw"}, cfg(on, off, off, off)) {
		t.Error("expected no trigger when nsfw individually disabled")
	}
}

func TestAdultContentOtherTagStillActive(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true, Disabled: []string{"nsfw"}}
	if !itchio.IsAdvisoryTriggered([]string{"porn"}, cfg(on, off, off, off)) {
		t.Error("expected trigger for 'porn' even when 'nsfw' individually disabled")
	}
}

// ── Queer Content ─────────────────────────────────────────────────────────────

func TestQueerContentMatch(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if !itchio.IsAdvisoryTriggered([]string{"queer"}, cfg(off, on, off, off)) {
		t.Error("expected trigger for queer content tag 'queer'")
	}
}

func TestQueerContentMasterDisabled(t *testing.T) {
	if itchio.IsAdvisoryTriggered([]string{"gay"}, cfg(off, off, off, off)) {
		t.Error("expected no trigger when queer content filter disabled")
	}
}

func TestQueerContentTagIndividuallyDisabled(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true, Disabled: []string{"lgbtq"}}
	if itchio.IsAdvisoryTriggered([]string{"lgbtq"}, cfg(off, on, off, off)) {
		t.Error("expected no trigger when lgbtq tag individually disabled")
	}
}

func TestQueerContentOtherTagStillActive(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true, Disabled: []string{"lgbtq"}}
	if !itchio.IsAdvisoryTriggered([]string{"gay"}, cfg(off, on, off, off)) {
		t.Error("expected trigger for 'gay' even when 'lgbtq' individually disabled")
	}
}

func TestQueerContentExpandedTags(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	for _, tag := range []string{"bisexual", "trans", "non-binary", "pansexual", "sapphic"} {
		if !itchio.IsAdvisoryTriggered([]string{tag}, cfg(off, on, off, off)) {
			t.Errorf("expected trigger for queer content tag %q", tag)
		}
	}
}

// ── Heavy Themes ──────────────────────────────────────────────────────────────

func TestHeavyThemesMatch(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if !itchio.IsAdvisoryTriggered([]string{"suicide"}, cfg(off, off, on, off)) {
		t.Error("expected trigger for heavy theme tag 'suicide'")
	}
}

func TestHeavyThemesDisabled(t *testing.T) {
	if itchio.IsAdvisoryTriggered([]string{"suicide"}, cfg(off, off, off, off)) {
		t.Error("expected no trigger when heavy themes filter disabled")
	}
}

func TestHeavyThemesTagIndividuallyDisabled(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true, Disabled: []string{"grief"}}
	if itchio.IsAdvisoryTriggered([]string{"grief"}, cfg(off, off, on, off)) {
		t.Error("expected no trigger when grief individually disabled")
	}
}

// ── Substance Use ─────────────────────────────────────────────────────────────

func TestSubstanceUseMatch(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if !itchio.IsAdvisoryTriggered([]string{"drugs"}, cfg(off, off, off, on)) {
		t.Error("expected trigger for substance use tag 'drugs'")
	}
}

func TestSubstanceUseDisabled(t *testing.T) {
	if itchio.IsAdvisoryTriggered([]string{"drugs"}, cfg(off, off, off, off)) {
		t.Error("expected no trigger when substance use filter disabled")
	}
}

// ── Cross-category ────────────────────────────────────────────────────────────

func TestAllFiltersOff(t *testing.T) {
	if itchio.IsAdvisoryTriggered([]string{"nsfw", "gay", "suicide", "drugs"},
		cfg(off, off, off, off)) {
		t.Error("expected no trigger when all filters disabled")
	}
}

func TestNonFlaggedTag(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if itchio.IsAdvisoryTriggered([]string{"platformer", "adventure"},
		cfg(on, on, on, on)) {
		t.Error("expected no trigger for non-flagged tags")
	}
}

func TestEmptyTags(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	if itchio.IsAdvisoryTriggered(nil, cfg(on, on, on, on)) {
		t.Error("expected no trigger for nil tags")
	}
	if itchio.IsAdvisoryTriggered([]string{}, cfg(on, on, on, on)) {
		t.Error("expected no trigger for empty tags")
	}
}

// ── Matched categories (F30) ──────────────────────────────────────────────────

// The warning names the categories that matched, spelled as the Content
// Moderation screen spells them, in that screen's order.
func TestMatchedCategoriesNamesTheCategories(t *testing.T) {
	on := itchio.CategoryFilter{Enabled: true}
	all := cfg(on, on, on, on)
	for _, tc := range []struct {
		name string
		tags []string
		cfg  itchio.FilterConfig
		want []string
	}{
		{"one category", []string{"adventure", "NSFW"}, all, []string{"Adult Content"}},
		{"two categories, in screen order", []string{"suicide", "nsfw"}, all, []string{"Adult Content", "Heavy Themes"}},
		{"all four", []string{"drugs", "grief", "queer", "porn"}, all,
			[]string{"Adult Content", "Queer Content", "Heavy Themes", "Substance Use"}},
		{"one category twice is named once", []string{"nsfw", "porn", "gore"}, all, []string{"Adult Content"}},
		{"a blocked-off category is not named", []string{"nsfw", "drugs"}, cfg(off, off, off, on), []string{"Substance Use"}},
		{"a tag allowed one by one is not named", []string{"nsfw"},
			cfg(itchio.CategoryFilter{Enabled: true, Disabled: []string{"nsfw"}}, off, off, off), nil},
		{"nothing matches", []string{"platformer"}, all, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := itchio.MatchedCategories(tc.tags, tc.cfg)
			if len(got) != len(tc.want) {
				t.Fatalf("MatchedCategories(%q) = %q, want %q", tc.tags, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("MatchedCategories(%q) = %q, want %q", tc.tags, got, tc.want)
				}
			}
			if triggered := itchio.IsAdvisoryTriggered(tc.tags, tc.cfg); triggered != (len(tc.want) > 0) {
				t.Fatalf("IsAdvisoryTriggered = %v with categories %q", triggered, tc.want)
			}
		})
	}
}

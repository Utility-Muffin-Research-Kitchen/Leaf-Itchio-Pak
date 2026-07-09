package itchio_test

import (
	"reflect"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

func TestFeedCodesAndSlugs(t *testing.T) {
	want := []itchio.FeedPlatform{
		{Code: "GBC", Name: "Game Boy Color", FeedSlugs: []string{"tag-gameboy-color", "tag-gbc"}},
		{Code: "GB", Name: "Game Boy", FeedSlugs: []string{"made-with-gb-studio", "tag-gbstudio", "tag-gameboy-rom"}},
		{Code: "GBA", Name: "Game Boy Advance", FeedSlugs: []string{"tag-gba"}},
		{Code: "NES", Name: "Nintendo Entertainment System", FeedSlugs: []string{"tag-nes-rom"}},
		{Code: "MD", Name: "Sega Genesis", FeedSlugs: []string{"tag-sega-mega-drive", "tag-genesis-rom"}},
		{Code: "P8", Name: "Pico-8", FeedSlugs: []string{"tag-pico-8"}},
	}

	if !reflect.DeepEqual(itchio.AllPlatforms, want) {
		t.Fatalf("AllPlatforms changed:\n got: %#v\nwant: %#v", itchio.AllPlatforms, want)
	}

	seenCodes := make(map[string]bool)
	seenSlugs := make(map[string]string)
	for _, platform := range itchio.AllPlatforms {
		if seenCodes[platform.Code] {
			t.Errorf("duplicate feed code %q", platform.Code)
		}
		seenCodes[platform.Code] = true
		for _, slug := range platform.FeedSlugs {
			if owner, exists := seenSlugs[slug]; exists {
				t.Errorf("feed slug %q is assigned to both %s and %s", slug, owner, platform.Code)
			}
			seenSlugs[slug] = platform.Code
		}
	}
}

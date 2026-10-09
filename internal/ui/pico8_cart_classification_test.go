//go:build !headless

package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// pngOfSize is a PNG w pixels wide and h high.
func pngOfSize(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pico8CartKinds7z is the 7z form of pico8CartKindsZIP, written by libarchive
// (bsdtar 3.7.4), since Go has no 7z writer:
//
//	bsdtar --format 7zip -cf kinds.7z game/main.p8 game/cart2.p8.png game/cart3.p8.png \
//	  game/shot game/cover.png game/label.png game/cartdata game/soulbound_v1_0 game/lib.lua
const pico8CartKinds7z = "N3q8ryccAAM63tCRuQEAAAAAAAAjAAAAAAAAAJ6zqPgAOBpIjiRWEBs71esuHjUZ+x1v86lyn5RdxaoeFEJcRc2ncSVw2Z+hQmmJuvTdwN6mr/8DpHuGQZZmmtgu/5cnjaXBUay6x641nP3zvrIAo2RfHNdb3otfOJsV6mpNKzTaH0MGqJ95Sc9ggrkyutIvK2mklW65lhfonJIcQEKLax/bQpxLGEQi5RrvnC2396G7siCiicXtwGETcmBMaNoBN9V02gg//9xFUQAAAIEzB64P1TCbX9ck0/6zfi+Jkr6+PNqYZWcM0/znvHulu9xAsj+y+qj/EBsP9s2M+OCOX+rr2EIBxt0vJ4hu+dEzYaLAlEgeWBl3gDA2L3LOBIHpyBIWDbtQFnrCSYe38DOjMwGLmZ4MXrfI/GzmSIzaf+Tp43Si0tU1M9gRsxk7lV4WnQimBa4NOyYj9yblHZ3ev/JZxXftRC4pl0EynRLaWhmAOJwWoAuAP1nGTNtLFKbF6mH078NOaXE+ZRAWXHX5NeRGjo0pUlss8EkRhkihOsNTqR2hVnuA9W7XrKdksjfu4Xmx3kiMDStcW+WCoBpFWETLjY9E4sIpG5omqvVNT7XUn2n//6cFQAAXBoCrAQmBDgAHCwEAASMDAQEFXQAAgAAMgnQKAeFoE8YAAA=="

// pico8CartKindsZIP holds, besides one Lua file, members of every kind the
// inspection once told apart from the extractors:
//
//   - main.p8, cart2.p8.png and cart3.p8.png are carts by name. The PNG ones
//     are real cart images, 160x205 pixels. (Two or more are needed: an archive
//     with one .p8.png and some .p8 files installs just the .p8.png.)
//   - soulbound_v1_0 has no Pico-8 name; its first bytes say it is a text cart.
//   - shot and cartdata have no extension. shot is 128 pixels wide, a Pico-8
//     screenshot or label; cartdata is a 160x205 PNG, the size of a cart image.
//   - cover.png and label.png are ordinary images (128 and 160x205).
func pico8CartKindsZIP(t *testing.T) []byte {
	return zipOf(t, map[string][]byte{
		"game/main.p8":        []byte("pico-8 cartridge // MAIN\nversion 41\n"),
		"game/cart2.p8.png":   pngOfSize(t, 160, 205),
		"game/cart3.p8.png":   pngOfSize(t, 160, 205),
		"game/shot":           pngOfSize(t, 128, 128),
		"game/cover.png":      pngOfSize(t, 128, 128),
		"game/label.png":      pngOfSize(t, 160, 205),
		"game/cartdata":       pngOfSize(t, 160, 205),
		"game/soulbound_v1_0": []byte("pico-8 cartridge // SOUL\nversion 41\n"),
		"game/lib.lua":        []byte("-- lib\n"),
	})
}

// inspectedCarts lists, by base name, the members the inspection counts as
// Pico-8 carts, sorted.
func inspectedCarts(manifest roms.ZIPManifest) []string {
	var carts []string
	for _, entry := range manifest.Entries {
		ext := strings.ToLower(roms.ROMExt(entry.Name))
		if entry.Kind == roms.KindROM && (ext == ".p8" || ext == ".p8.png") {
			carts = append(carts, filepath.Base(entry.Name))
		}
	}
	sort.Strings(carts)
	return carts
}

// writtenCarts lists, by base name, the carts a game folder holds, sorted.
func writtenCarts(tree map[string]string) []string {
	var carts []string
	for name := range tree {
		ext := strings.ToLower(roms.ROMExt(name))
		if ext == ".p8" || ext == ".p8.png" {
			carts = append(carts, filepath.Base(name))
		}
	}
	sort.Strings(carts)
	return carts
}

// inspectRemotely runs the inspection the download flow runs before it
// downloads an archive.
func inspectRemotely(t *testing.T, name string, data []byte) roms.ZIPManifest {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	var (
		manifest roms.ZIPManifest
		err      error
	)
	if strings.HasSuffix(name, ".7z") {
		manifest, err = roms.InspectRemote7z(srv.Client(), srv.URL)
	} else {
		manifest, err = roms.InspectRemoteZIP(srv.Client(), srv.URL, nil)
	}
	if err != nil {
		t.Fatalf("%s: inspection: %v", name, err)
	}
	return manifest
}

// F27: the inspection and the multi-file extractors choose the same carts,
// whatever the archive type. A member is a cart when its name says so
// (.p8 or .p8.png), or when it has no image name and starts like a text cart
// ("pico-8 cartridge"). A PNG is never a cart by its bytes: a cart image is
// 160x205 pixels, so the old check for a PNG 128 pixels wide matched Pico-8
// screenshots and labels, and a 160x205 PNG is as likely a cover. The
// extractors once chose by name alone, so a text cart without a Pico-8 name
// was counted and then not installed, and a PNG without an extension was
// counted as a second .p8.png cart and then not installed.
func TestInspectionAndMultiFileExtractorsAgreeOnWhichMembersAreCarts(t *testing.T) {
	archives := map[string][]byte{"kinds.zip": pico8CartKindsZIP(t), "kinds.7z": decode7z(t, pico8CartKinds7z)}
	for name, data := range archives {
		t.Run(name, func(t *testing.T) {
			manifest := inspectRemotely(t, name, data)
			inspected := inspectedCarts(manifest)

			// The routing sees the carts the inspection counts: three carts
			// and a Lua file are a multi-file game.
			flow := archiveFlowFixture(t, &settings.Config{ROMLocation: "auto"}, manifest)
			flow.prepareInitialAction()
			if flow.plan.Pico8GameDir == "" {
				t.Fatalf("the archive is not routed to the multi-file extractor")
			}

			// The extractor writes those carts, and no other member.
			primary, _ := transactionPaths(t)
			inv, invPath := collisionInventory(t)
			gameDir := filepath.Join(primary, "Roms", "PICO8", "Moss Garden")
			worker := runArchiveFor(t, primary, ownerGame, inv, invPath, name, data, &settings.Config{},
				func(plan *ZIPPlan, _ string) { plan.Pico8GameDir = gameDir + string(filepath.Separator) })
			tree := treeOf(t, gameDir)
			written := writtenCarts(tree)
			if fmt.Sprint(inspected) != fmt.Sprint(written) {
				t.Fatalf("the inspection counts %v as carts, the extractor wrote %v (skipped %v)", inspected, written, worker.skipped)
			}
			if want := "[cart2.p8.png cart3.p8.png main.p8 soulbound_v1_0.p8]"; fmt.Sprint(written) != want {
				t.Fatalf("carts = %v, want %s", written, want)
			}
			for _, rel := range []string{"lib.lua", "soulbound_v1_0.p8"} {
				if _, ok := tree[rel]; !ok {
					t.Fatalf("game folder = %v, want %s", keys(tree), rel)
				}
			}
			for rel := range tree {
				base := strings.ToLower(filepath.Base(rel))
				if strings.HasPrefix(base, "shot") || strings.HasPrefix(base, "cover") ||
					strings.HasPrefix(base, "label") || strings.HasPrefix(base, "cartdata") {
					t.Fatalf("game folder holds %s, which is not a cart", rel)
				}
			}
		})
	}
}

// F27: an archive's only cart image beside a text cart is installed as a
// plain pair; a PNG with no cart name does not make a second .p8.png and push
// it into the multi-file extractor.
func TestAPNGWithoutACartNameIsNotACart(t *testing.T) {
	data := zipOf(t, map[string][]byte{
		"game/cart.p8.png": pngOfSize(t, 160, 205),
		"game/level2.p8":   []byte("pico-8 cartridge // LEVEL2\n"),
		"game/data":        pngOfSize(t, 128, 128),
	})
	manifest := inspectRemotely(t, "leafbound.zip", data)
	if got := fmt.Sprint(inspectedCarts(manifest)); got != "[cart.p8.png level2.p8]" {
		t.Fatalf("the inspection counts %s as carts, want the two named ones", got)
	}
	flow := archiveFlowFixture(t, &settings.Config{ROMLocation: "auto"}, manifest)
	flow.prepareInitialAction()
	if flow.plan.Pico8GameDir != "" {
		t.Fatalf("one cart image and one text cart are routed to the multi-file extractor")
	}

	// Forced through the multi-file extractor, nothing but the named carts is written.
	_, dir := runArchive(t, "leafbound.zip", data, &settings.Config{}, true)
	names := []string{}
	for name := range treeOf(t, dir) {
		if !isPlaylist(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if fmt.Sprint(names) != "[cart.p8.png level2.p8]" {
		t.Fatalf("game folder = %v, want the carts under their own names", names)
	}
}

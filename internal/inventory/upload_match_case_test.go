package inventory_test

import (
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// F18: an installed upload named in capitals still matches the page's name
// for it, which has no extension.
func TestInstalledUploadMatchesAListedNameWhateverTheExtensionCase(t *testing.T) {
	for name, listed := range map[string]string{"cart.p8.png": "cart", "CART.P8.PNG": "CART", "Cart.P8.png": "Cart"} {
		entry := &inventory.Entry{
			Title: "Cart",
			Files: []inventory.DownloadedFile{{Filename: name, OriginalUpload: name, DestPath: "/leaf/Roms/PICO8/" + name}},
			KnownUpstreamFiles: []inventory.UpstreamFile{
				{Filename: listed, IsNew: true, SeenAt: time.Now()},
			},
		}
		inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{"https://dev.itch.io/cart": entry}}
		if inv.HasPendingUpdates("https://dev.itch.io/cart") {
			t.Errorf("%s: an upload you installed shows as new", name)
		}
	}
}

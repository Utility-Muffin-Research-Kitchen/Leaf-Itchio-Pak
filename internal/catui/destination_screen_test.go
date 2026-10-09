package catui

import (
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

// F32: every destination screen puts its select or confirm hint in the right
// group, as the Manage list, Settings and the download picker do, and keeps
// B Back on the left.
func TestDestinationFooterPutsTheConfirmHintOnTheRight(t *testing.T) {
	sources := appui.NewDestinationModel("Hidden palace")
	sources.SetSources([]appui.DestinationItem{{Kind: appui.DestinationItemSource, Label: "Primary SD", Enabled: true}})
	folders := appui.NewDestinationModel("Hidden palace")
	folders.SetFolders("Choose GBC folder (1/2)", "Roms/GBC", []appui.DestinationItem{
		{Kind: appui.DestinationItemFolder, Label: "RPG", Enabled: true}, {Kind: appui.DestinationItemSave, Enabled: true},
	})
	save := appui.NewDestinationModel("Hidden palace")
	save.SetFolders("Choose GBC folder (1/2)", "Roms/GBC", []appui.DestinationItem{
		{Kind: appui.DestinationItemSave, Enabled: true}, {Kind: appui.DestinationItemFolder, Label: "RPG", Enabled: true},
	})
	confirm := appui.NewDestinationModel("Hidden palace")
	confirm.SetConfirm("Confirm", "Roms/GBC", nil)
	unavailable := appui.NewDestinationModel("Hidden palace")
	unavailable.SetError("Secondary SD isn't available.")

	for _, tc := range []struct {
		name  string
		model *appui.DestinationModel
		label string // the A hint, or "" when the screen has none
	}{
		{"card picker", sources, "Select"},
		{"folder picker on a folder", folders, "Select"},
		{"folder picker on Save here", save, "Save here"},
		{"confirm", confirm, "Download"},
		{"unavailable", unavailable, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hints := destinationFooter(tc.model)
			buttons := footerButtons(hints)
			if back, ok := buttons[ButtonB]; !ok || back.Label != "Back" || back.IsConfirm {
				t.Fatalf("B hint = %+v (present %v), want Back in the left group", back, ok)
			}
			a, ok := buttons[ButtonA]
			if tc.label == "" {
				if ok {
					t.Fatalf("A hint = %+v, want none", a)
				}
				return
			}
			if !ok || a.Label != tc.label {
				t.Fatalf("A hint = %+v (present %v), want %q", a, ok, tc.label)
			}
			if !a.IsConfirm {
				t.Fatalf("A %s sits in the left group next to B Back; want it in the right group", a.Label)
			}
		})
	}
}

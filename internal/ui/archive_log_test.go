//go:build !headless

package ui

import (
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// An archive install logs its extractions and identical-file skips under its
// own archive type. ZIP installs used to log both as "7z-download:".
func TestArchiveInstallLogsUnderItsArchiveType(t *testing.T) {
	for name, data := range map[string][]byte{
		"leafbound.zip": zipOf(t, map[string][]byte{"leafbound.gb": gbROM("MAIN")}),
		// romsCollision7z holds extra.dat and leafbound.gb.
		"leafbound.7z": decode7z(t, romsCollision7z),
	} {
		t.Run(name, func(t *testing.T) {
			want, other := "zip-download:", "7z-download:"
			if strings.HasSuffix(name, ".7z") {
				want, other = other, want
			}
			primary, _ := transactionPaths(t)
			inv, invPath := collisionInventory(t)
			logs := captureLogs(t)
			runArchiveFor(t, primary, collisionGame, inv, invPath, name, data, &settings.Config{})
			// The reinstall finds every ROM already in place.
			runArchiveFor(t, primary, collisionGame, inv, invPath, name, data, &settings.Config{})

			got := logs.String()
			if !hasLogLine(got, want, "ROM extracted") {
				t.Errorf("no %q extraction line in:\n%s", want, got)
			}
			if !hasLogLine(got, want, "identical file at") {
				t.Errorf("no %q identical-file line in:\n%s", want, got)
			}
			if strings.Contains(got, other) {
				t.Errorf("a %s install logged %q:\n%s", name, other, got)
			}
		})
	}
}

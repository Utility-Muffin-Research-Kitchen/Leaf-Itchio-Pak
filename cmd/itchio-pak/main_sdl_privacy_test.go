//go:build !headless

package main

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

func TestRuntimeLogPathsUseSourceRelativeLabels(t *testing.T) {
	var output bytes.Buffer
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
		for _, label := range []string{
			"[APP-DATA]", "[LOGS]", "[USERDATA]", "[LEAF-PLATFORM]",
			"[LEAF-CONTROL]", "[JAWAKA-RUNTIME]", "[SD:primary]", "[SD:secondary_sd]",
		} {
			logger.RemovePrivatePath(label)
		}
	})

	environment := leaf.Environment{
		UserdataPath:     "/private/cards/primary/.userdata/mlp1",
		LogsPath:         "/private/cards/primary/.userdata/mlp1/logs",
		PlatformPath:     "/private/cards/primary/.system/leaf/platforms/mlp1",
		InternalDataPath: "/private/cards/primary/.umrk/mlp1",
		RuntimePath:      "/private/runtime/jawaka",
		Sources: leaf.SourceList{
			{ID: "primary", Root: "/private/cards/primary", Primary: true},
			{ID: "secondary_sd", Root: "/private/cards/secondary"},
		},
	}
	registerRuntimeLogPaths(environment)
	logger.Info("config=%s rom=%s", environment.StateDir()+"/config.json", environment.Sources[1].Root+"/Roms/GBC/Leafbound.gbc")

	out := output.String()
	for _, forbidden := range []string{environment.StateDir(), environment.Sources[1].Root, "/private/cards"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("runtime log leaked %q: %s", forbidden, out)
		}
	}
	for _, want := range []string{"[APP-DATA]/config.json", "[SD:secondary_sd]/Roms/GBC/Leafbound.gbc"} {
		if !strings.Contains(out, want) {
			t.Errorf("runtime log missing %q: %s", want, out)
		}
	}
}

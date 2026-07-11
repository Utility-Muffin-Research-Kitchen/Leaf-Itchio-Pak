package catui

import (
	"fmt"
	"os"
	"runtime"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

type InputFixtureConfig struct {
	Screen         string
	Frames         int
	ScreenshotPath string
}

func RunInputFixture(config InputFixtureConfig) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, err := Init(Config{
		Title:            "Itch.io input migration",
		FontPath:         os.Getenv("CAT_FONT_PATH"),
		FallbackFontsDir: os.Getenv("ITCHIO_RES_DIR"),
	})
	if err != nil {
		return err
	}
	defer ctx.Close()

	filterModel := appui.NewFilterModel("GBC", "paid", "leaf 葉")
	filterModel.Section = appui.FilterPlatform
	filter, err := NewFilterScreen(ctx, filterModel)
	if err != nil {
		return err
	}
	draw := func() error {
		switch config.Screen {
		case "filter":
			return filter.Draw()
		default:
			return fmt.Errorf("unknown input fixture %q", config.Screen)
		}
	}
	handle := func(event InputEvent) (bool, error) {
		if event.Wake {
			return true, nil
		}
		switch config.Screen {
		case "filter":
			return filter.HandleInput(event) != appui.FilterIntentCancel, nil
		default:
			return false, fmt.Errorf("unknown input fixture %q", config.Screen)
		}
	}

	running, redraw, drawn := true, true, 0
	for running {
		for {
			event, ok, pollErr := ctx.PollInput()
			if pollErr != nil {
				return pollErr
			}
			if !ok {
				break
			}
			running, err = handle(event)
			if err != nil {
				return err
			}
			redraw = true
		}
		if !running {
			break
		}
		if redraw {
			if err := draw(); err != nil {
				return err
			}
			drawn++
			if config.Frames > 0 && drawn >= config.Frames {
				if config.ScreenshotPath != "" {
					if err := ctx.BeginCapture(); err != nil {
						return err
					}
					if err := draw(); err != nil {
						_ = ctx.EndCapture()
						return err
					}
					if err := ctx.ScreenshotPNG(config.ScreenshotPath); err != nil {
						_ = ctx.EndCapture()
						return err
					}
					if err := ctx.EndCapture(); err != nil {
						return err
					}
				}
				ctx.RequestFrame()
				return ctx.Present()
			}
			redraw = false
		}
		if config.Frames > 0 {
			ctx.RequestFrame()
			redraw = true
		}
		if err := ctx.Present(); err != nil {
			return err
		}
	}
	return nil
}

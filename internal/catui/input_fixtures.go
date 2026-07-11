package catui

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"time"

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

	cache := NewImageCache(8, nil)
	defer cache.Clear()
	cache.SetNotify(func() { _ = ctx.Wake() })
	delays := []time.Duration{120 * time.Millisecond, 120 * time.Millisecond}
	if config.Frames == 1 {
		delays = []time.Duration{10 * time.Second, 10 * time.Second}
	}
	if err := cache.Seed(ctx, "fixture://detail-cover", []image.Image{fixtureImage(0), fixtureImage(1)}, delays); err != nil {
		return err
	}
	if err := cache.Seed(ctx, "fixture://detail-shot", []image.Image{fixtureImage(2)}, nil); err != nil {
		return err
	}

	var draw func() error
	var handleIntent func(InputEvent) bool
	var closeScreen func()
	switch config.Screen {
	case "filter":
		model := appui.NewFilterModel("GBC", "paid", "leaf 葉")
		model.Section = appui.FilterPlatform
		screen, screenErr := NewFilterScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.FilterIntentCancel
		}
	case "detail", "warning":
		model := appui.NewDetailModel(appui.DetailGame{
			Title: "Leafbound 葉", Author: "UMRK fixture", URL: "https://example.itch.io/leafbound",
			Platform: "GBC", IsFree: true,
		})
		model.SetReady(`<h2>A pocket-sized journey</h2><p>Explore a multilingual forest, collect lost seeds, and bring music back to every clearing.</p><ul><li>Controller ready</li><li>Offline after install</li></ul>`,
			[]string{"Game Boy Color", "Adventure", "日本語", "GIF gallery"},
			[]string{"fixture://detail-cover", "fixture://detail-shot"}, false, config.Screen == "warning")
		screen, screenErr := NewDetailScreen(ctx, model, cache)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		closeScreen = screen.Close
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.DetailIntentBack
		}
	default:
		return fmt.Errorf("unknown input fixture %q", config.Screen)
	}
	if closeScreen != nil {
		defer closeScreen()
	}
	handle := func(event InputEvent) (bool, error) {
		if event.Wake {
			return true, nil
		}
		return handleIntent(event), nil
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
		if uploaded, processErr := cache.ProcessPending(ctx); processErr != nil {
			return processErr
		} else if uploaded {
			redraw = true
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
		if delay, animated := cache.NextFrameIn(); animated {
			milliseconds := delay.Milliseconds()
			if milliseconds < 1 {
				milliseconds = 1
			}
			ctx.RequestFrameIn(uint32(milliseconds))
			redraw = true
		}
		if err := ctx.Present(); err != nil {
			return err
		}
	}
	return nil
}

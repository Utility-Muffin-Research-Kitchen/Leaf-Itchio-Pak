package catui

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

type MainListScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.MainListModel
	cache *ImageCache
}

func NewMainListScreen(ctx *Context, model *appui.MainListModel, cache *ImageCache) (*MainListScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &MainListScreen{ctx: ctx, ui: ui, model: model, cache: cache}, nil
}

func (screen *MainListScreen) HandleInput(event InputEvent) appui.ListIntent {
	if event.Wake {
		return appui.ListIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button:   appButton(event.Button),
		Pressed:  event.Pressed,
		Repeated: event.Repeated,
	})
}

func appButton(button Button) appui.Button {
	switch button {
	case ButtonUp:
		return appui.ButtonUp
	case ButtonDown:
		return appui.ButtonDown
	case ButtonLeft:
		return appui.ButtonLeft
	case ButtonRight:
		return appui.ButtonRight
	case ButtonA:
		return appui.ButtonA
	case ButtonB, ButtonMenu:
		return appui.ButtonB
	case ButtonX:
		return appui.ButtonX
	case ButtonL1:
		return appui.ButtonL1
	case ButtonR1:
		return appui.ButtonR1
	case ButtonStart:
		return appui.ButtonStart
	case ButtonSelect:
		return appui.ButtonSelect
	case ButtonQuit:
		return appui.ButtonQuit
	default:
		return appui.ButtonNone
	}
}

func (screen *MainListScreen) Draw() error {
	footer := []FooterHint{{Button: ButtonB, Label: "Exit"}}
	switch screen.model.State {
	case appui.ListError:
		footer = append(footer, FooterHint{Button: ButtonA, Label: "Retry", IsConfirm: true})
	case appui.ListReady, appui.ListEmpty:
		footer = []FooterHint{
			{Button: ButtonB, Label: "Exit"},
			{Button: ButtonSelect, Label: "Filter"},
			{Button: ButtonL1, Label: "Previous sort", NarrowLabel: "Sort -"},
			{Button: ButtonR1, Label: "Next sort", NarrowLabel: "Sort +"},
			{Button: ButtonStart, Label: "Settings", NarrowLabel: "Set"},
			{Button: ButtonA, Label: "Open", IsConfirm: true},
		}
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title:           "Itch.io",
		SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10),
		Footer:          footer,
	})
	if err != nil {
		return err
	}
	subtitle := fmt.Sprintf("All platforms  ·  Newest  ·  %d games", len(screen.model.Items))
	if err := screen.ui.DrawSubHeader(frame.Layout.SubHeader, subtitle); err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	switch screen.model.State {
	case appui.ListLoading:
		if err := screen.ui.DrawState(body, StateLoading, "Loading games", "Reading the itch.io feed and local cache…"); err != nil {
			return err
		}
	case appui.ListError:
		detail := screen.model.ErrorDetail
		if detail == "" {
			detail = "The feed could not be loaded. Check the network and retry."
		}
		if err := screen.ui.DrawState(body, StateError, "Could not load games", detail); err != nil {
			return err
		}
	case appui.ListEmpty:
		if err := screen.ui.DrawState(body, StateEmpty, "No matching games", "Use SELECT to change the active filters."); err != nil {
			return err
		}
	default:
		if err := screen.drawReady(frame.Layout.Content); err != nil {
			return err
		}
	}
	return frame.Finish()
}

func (screen *MainListScreen) drawReady(content Box) error {
	split := ListDetailSplit(content, 58, screen.ui.BasePadding)
	geometry := FitScrollingList(split.List, screen.ctx.FontHeight(FontMedium)+screen.ctx.Scale(14), len(screen.model.Items), 0)
	screen.model.VisibleRows = geometry.VisibleRows
	start := screen.model.Cursor - geometry.VisibleRows + 1
	if start < 0 {
		start = 0
	}
	for row := 0; row < geometry.VisibleRows && start+row < len(screen.model.Items); row++ {
		index := start + row
		item := screen.model.Items[index]
		if err := screen.ui.DrawListRow(geometry.Row(row), item.Title, item.Badge, index == screen.model.Cursor); err != nil {
			return err
		}
	}
	selected, ok := screen.model.Selected()
	if !ok {
		return nil
	}
	panel := split.Detail.Content()
	artHeight := panel.H * 58 / 100
	art := Rect{X: panel.X, Y: panel.Y, W: panel.W, H: artHeight}
	if selected.CoverKey == "" {
		if err := screen.ui.DrawState(art, StateEmpty, "No image", ""); err != nil {
			return err
		}
	} else if texture := screen.cache.Get(selected.CoverKey); texture != nil {
		if err := screen.ui.DrawImageFit(texture, art); err != nil {
			return err
		}
	} else if screen.cache.Failed(selected.CoverKey) {
		if err := screen.ui.DrawState(art, StateError, "No image", "Artwork could not be decoded."); err != nil {
			return err
		}
	} else if err := screen.ui.DrawState(art, StateLoading, "Loading artwork", ""); err != nil {
		return err
	}

	y := art.Y + art.H + screen.ui.BasePadding/2
	remaining := Rect{X: panel.X, Y: y, W: panel.W, H: panel.Y + panel.H - y}
	if _, err := screen.ctx.DrawFallbackText(FontLarge, selected.Title, remaining.X, remaining.Y,
		screen.ctx.ThemeColor(RoleEmphasis), remaining.W); err != nil {
		return err
	}
	y += screen.ctx.FontHeight(FontLarge) + screen.ctx.Scale(4)
	if selected.Author != "" {
		if _, err := screen.ctx.DrawFallbackText(FontSmall, "by "+selected.Author, remaining.X, y,
			screen.ctx.ThemeColor(RoleHint), remaining.W); err != nil {
			return err
		}
		y += screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(8)
	}
	if y < remaining.Y+remaining.H {
		tags := make([]string, 0, len(selected.Tags))
		for _, tag := range selected.Tags {
			if tag != "" && !strings.EqualFold(tag, "free") {
				tags = append(tags, tag)
			}
		}
		_, err := screen.ui.DrawTagPills(Rect{X: remaining.X, Y: y, W: remaining.W, H: remaining.Y + remaining.H - y}, tags)
		return err
	}
	return nil
}

type MainListFixtureConfig struct {
	State          string
	Frames         int
	ScreenshotPath string
}

func RunMainListFixture(config MainListFixtureConfig) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, err := Init(Config{
		Title:            "Itch.io main-list migration",
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
	animationDelays := []time.Duration{120 * time.Millisecond, 120 * time.Millisecond, 120 * time.Millisecond}
	if config.Frames == 1 {
		// Snapshot lanes must capture the same first frame regardless of host
		// startup speed. Interactive and multi-frame runs still animate normally.
		animationDelays = []time.Duration{10 * time.Second, 10 * time.Second, 10 * time.Second}
	}
	if err := cache.Seed(ctx, "fixture://animated-cover", []image.Image{
		fixtureImage(0), fixtureImage(1), fixtureImage(2),
	}, animationDelays); err != nil {
		return err
	}
	for index := 1; index < 3; index++ {
		if err := cache.Seed(ctx, fmt.Sprintf("fixture://cover-%d", index), []image.Image{fixtureImage(index)}, nil); err != nil {
			return err
		}
	}
	items := fixtureListItems()
	model := appui.NewMainListModel(items)
	switch strings.ToLower(config.State) {
	case "loading":
		model.SetLoading()
	case "error":
		model.SetError("Cloudflare blocked the feed. Visit itch.io on this network, then retry.")
	case "empty":
		model.SetItems(nil)
	}
	screen, err := NewMainListScreen(ctx, model, cache)
	if err != nil {
		return err
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
			if event.Wake {
				redraw = true
				continue
			}
			switch screen.HandleInput(event) {
			case appui.ListIntentExit:
				running = false
			case appui.ListIntentRetry:
				model.SetLoading()
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
			if err := screen.Draw(); err != nil {
				return err
			}
			drawn++
			if config.Frames > 0 && drawn >= config.Frames {
				if config.ScreenshotPath != "" {
					if err := ctx.ScreenshotPNG(config.ScreenshotPath); err != nil {
						return err
					}
				}
				// Read back the complete frame before swapping buffers. Some MLP1
				// SDL backends do not preserve the new backbuffer after Present;
				// redrawing solely for a screenshot can therefore capture stale
				// rectangles even though the presented frame is correct.
				ctx.RequestFrame()
				if err := ctx.Present(); err != nil {
					return err
				}
				break
			}
			redraw = false
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

func fixtureListItems() []appui.ListItem {
	titles := []string{
		"A Short Hike", "Celeste Classic", "Dungeons of Dreadrock", "Frog Detective",
		"Goodboy Galaxy", "Haiku, the Robot", "Into the Breach Demake", "Micro Mages",
		"Night in the Woods", "Old School Rally", "Pico-8 Collection", "Quest of Graal",
		"Retro City Rampage", "Super Crate Box", "Tiny Dangerous Dungeons", "VVVVVV",
	}
	items := make([]appui.ListItem, 0, len(titles))
	for index, title := range titles {
		cover := fmt.Sprintf("fixture://cover-%d", index%3)
		if index == 0 {
			cover = "fixture://animated-cover"
		}
		badge := "Free"
		if index%5 == 0 {
			badge = "DL"
		} else if index%4 == 0 {
			badge = "$4.99"
		}
		items = append(items, appui.ListItem{
			Title: title, Author: "itch creator", CoverKey: cover, Badge: badge,
			Tags: []string{"Game Boy", "Adventure", "Controller"},
		})
	}
	return items
}

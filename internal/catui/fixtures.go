package catui

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"runtime"
	"time"
)

const FixturePageCount = 5

type FixtureConfig struct {
	Page           int
	Frames         int
	ScreenshotPath string
}

func RunFixtures(config FixtureConfig) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctx, err := Init(Config{
		Title:            "Itch.io shared primitive fixtures",
		FontPath:         os.Getenv("CAT_FONT_PATH"),
		FallbackFontsDir: os.Getenv("ITCHIO_RES_DIR"),
	})
	if err != nil {
		return err
	}
	defer ctx.Close()
	wrongThread := make(chan error, 1)
	go func() {
		wrongThread <- ctx.DrawRect(Rect{W: 1, H: 1}, RGBA(0, 0, 0, 0))
	}()
	if threadErr := <-wrongThread; !errors.Is(threadErr, ErrWrongThread) {
		return fmt.Errorf("Catastrophe owner-thread guard: got %v, want %v", threadErr, ErrWrongThread)
	}
	if err := verifyFixtureWorkerWake(ctx); err != nil {
		return err
	}
	if err := verifyFixtureInputFrames(ctx); err != nil {
		return err
	}
	if err := verifyFixtureTextCache(ctx); err != nil {
		return err
	}
	ui, err := NewComposer(ctx)
	if err != nil {
		return err
	}

	textures := make([]*Texture, 0, 3)
	for index := 0; index < 3; index++ {
		texture, textureErr := ctx.TextureFromImage(fixtureImage(index))
		if textureErr != nil {
			return textureErr
		}
		textures = append(textures, texture)
	}

	page := config.Page
	if page < 0 || page >= FixturePageCount {
		page = 0
	}
	running, redraw, drawnFrames := true, true, 0
	for running {
		for {
			event, ok, pollErr := ctx.PollInput()
			if pollErr != nil {
				return pollErr
			}
			if !ok {
				break
			}
			if event.Wake || !event.Pressed {
				continue
			}
			switch event.Button {
			case ButtonLeft, ButtonUp:
				page = (page + FixturePageCount - 1) % FixturePageCount
				redraw = true
			case ButtonRight, ButtonDown, ButtonA:
				page = (page + 1) % FixturePageCount
				redraw = true
			case ButtonB, ButtonMenu, ButtonQuit:
				running = false
			}
		}
		if !running {
			break
		}
		if redraw {
			if err := drawFixturePage(ui, textures, page); err != nil {
				return err
			}
			drawnFrames++
			if config.Frames > 0 && drawnFrames >= config.Frames {
				// Present the acceptance frame once so the real display and any
				// batched backend work are exercised. Redraw the same complete
				// frame into the new backbuffer before deterministic readback.
				ctx.RequestFrame()
				if err := ctx.Present(); err != nil {
					return err
				}
				if config.ScreenshotPath != "" {
					if err := drawFixturePage(ui, textures, page); err != nil {
						return err
					}
					if err := ctx.ScreenshotPNG(config.ScreenshotPath); err != nil {
						return err
					}
				}
				break
			}
			if config.Frames > 0 {
				page = (page + 1) % FixturePageCount
				redraw = true
				ctx.RequestFrame()
			} else {
				redraw = false
			}
		}
		if err := ctx.Present(); err != nil {
			return err
		}
	}
	return nil
}

func drawFixturePage(ui *Composer, textures []*Texture, page int) error {
	switch page {
	case 0:
		return drawListFixture(ui, textures[0])
	case 1:
		return drawOverlayFixture(ui)
	case 2:
		return drawInputFixture(ui)
	case 3:
		return drawGalleryFixture(ui, textures)
	case 4:
		return drawStatesFixture(ui)
	default:
		return fmt.Errorf("catui: fixture page %d", page)
	}
}

func fixtureFooter() []FooterHint {
	return []FooterHint{
		{Button: ButtonB, Label: "Previous page", NarrowLabel: "Prev"},
		{Button: ButtonA, Label: "Next fixture page", NarrowLabel: "Next", IsConfirm: true},
	}
}

func drawListFixture(ui *Composer, texture *Texture) error {
	frame, err := ui.BeginScreen(ScreenSpec{
		Title:           "List primitives",
		SubHeaderHeight: ui.ctx.FontHeight(FontSmall) + ui.ctx.Scale(10),
		Footer:          fixtureFooter(),
	})
	if err != nil {
		return err
	}
	if err := ui.DrawSubHeader(frame.Layout.SubHeader, "List/detail · fitted rows · values · tags · image fit"); err != nil {
		return err
	}
	split := ListDetailSplit(frame.Layout.Content, 58, ui.BasePadding)
	geometry := FitScrollingList(split.List, ui.ctx.FontHeight(FontMedium)+ui.ctx.Scale(14), 8, 0)
	rows := []struct {
		primary, secondary string
	}{
		{"Fresh releases", "24"},
		{"Owned games", "128"},
		{"Downloaded", "12"},
	}
	for index, row := range rows {
		if err := ui.DrawListRow(geometry.Row(index), row.primary, row.secondary, index == 0); err != nil {
			return err
		}
	}
	if err := ui.DrawValueRow(geometry.Row(3), "Sort order", "Newest", false, true); err != nil {
		return err
	}

	detail := split.Detail
	artBox := detail.CarveTop(detail.Content().H * 56 / 100)
	if err := ui.DrawImageFit(texture, artBox.Content()); err != nil {
		return err
	}
	tags := detail.Content()
	if _, err := ui.DrawTagPills(tags, []string{"葉っぱ", "dual SD", "GIF", "offline-ready", "Catastrophe"}); err != nil {
		return err
	}
	return frame.Finish()
}

func drawOverlayFixture(ui *Composer) error {
	frame, err := ui.BeginScreen(ScreenSpec{Title: "Body + overlays", Footer: fixtureFooter()})
	if err != nil {
		return err
	}
	body := FullWidthBody(frame.Layout.Content)
	if err := ui.DrawScrollingBody(body, "A clipped, full-width scrolling body", []string{
		"Remote descriptions wrap inside cat_box_content and remain clipped to their final-pixel viewport.",
		"The overlay examples below use named modal padding and live Catastrophe theme roles.",
	}, nil); err != nil {
		return err
	}
	overlayTop := body.Y + body.H*42/100
	overlayBounds := Rect{X: body.X, Y: overlayTop, W: body.W, H: body.Y + body.H - overlayTop}
	left, right := splitRect(overlayBounds, 50, ui.BasePadding)
	if err := ui.DrawWarningCover(left, "Content warning", "Filtered themes may be present. Review before continuing."); err != nil {
		return err
	}
	if err := ui.DrawModal(right, "Remove?", "Inventory and artwork update together."); err != nil {
		return err
	}
	return frame.Finish()
}

func drawInputFixture(ui *Composer) error {
	frame, err := ui.BeginScreen(ScreenSpec{
		Title: "Text input",
		Footer: []FooterHint{
			{Button: ButtonA, Label: "Confirm entered value", NarrowLabel: "Done", IsConfirm: true},
		},
	})
	if err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	fieldHeight := ui.ctx.FontHeight(FontSmall) + ui.ctx.Scale(20)
	field := Rect{X: body.X, Y: body.Y, W: body.W, H: fieldHeight}
	if err := ui.DrawTextField(field, "Search itch.io", "leaf games", true); err != nil {
		return err
	}
	keyboard := Rect{X: body.X, Y: field.Y + field.H + ui.BasePadding, W: body.W,
		H: body.Y + body.H - field.Y - field.H - ui.BasePadding}
	keys := [][]string{
		{"Q", "W", "E", "R", "T", "Y"},
		{"A", "S", "D", "F", "G", "H"},
		{"Z", "X", "C", "V", "B", "N"},
		{"123", "Space", "⌫", "Done"},
	}
	if err := ui.DrawKeyboard(keyboard, keys, 10); err != nil {
		return err
	}
	return frame.Finish()
}

func drawGalleryFixture(ui *Composer, textures []*Texture) error {
	frame, err := ui.BeginScreen(ScreenSpec{Title: "Gallery + progress", Footer: fixtureFooter()})
	if err != nil {
		return err
	}
	split := ListDetailSplit(frame.Layout.Content, 62, ui.BasePadding)
	if err := ui.DrawGallery(split.List.Content(), textures, 1); err != nil {
		return err
	}
	if err := ui.DrawProgressView(split.Detail.Content(), "Installing", "Primary Music root · track 7 of 12", 0.58); err != nil {
		return err
	}
	return frame.Finish()
}

func drawStatesFixture(ui *Composer) error {
	frame, err := ui.BeginScreen(ScreenSpec{Title: "Standard states", Footer: fixtureFooter()})
	if err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	left, right := splitRect(body, 50, ui.BasePadding)
	leftTop, leftBottom := splitRectRows(left, 50, ui.BasePadding)
	rightTop, rightBottom := splitRectRows(right, 50, ui.BasePadding)
	states := []struct {
		rect          Rect
		kind          StateKind
		title, detail string
	}{
		{leftTop, StateEmpty, "Nothing here yet", "Try another filter."},
		{rightTop, StateLoading, "Loading library", "Reading the local cache…"},
		{leftBottom, StateOffline, "You are offline", "Downloaded games remain available."},
		{rightBottom, StateError, "Request failed", "Press A to retry."},
	}
	for _, state := range states {
		if err := ui.ctx.DrawPill(Rect{X: state.rect.X + ui.ModalPadding, Y: state.rect.Y,
			W: maxInt(0, state.rect.W-ui.ModalPadding*2), H: ui.ctx.Scale(5)},
			ui.ctx.ThemeColor(RoleAccent)); err != nil {
			return err
		}
		if err := ui.DrawState(state.rect, state.kind, state.title, state.detail); err != nil {
			return err
		}
	}
	return frame.Finish()
}

func splitRect(bounds Rect, leftPercent, gap int) (Rect, Rect) {
	box := NewBox(bounds.X, bounds.Y, bounds.W, bounds.H, 0)
	leftBox, rightBox := box.SplitColumns(bounds.W*leftPercent/100, gap)
	return leftBox.Content(), rightBox.Content()
}

func splitRectRows(bounds Rect, topPercent, gap int) (Rect, Rect) {
	topHeight := maxInt(0, (bounds.H-gap)*topPercent/100)
	return Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: topHeight},
		Rect{X: bounds.X, Y: bounds.Y + topHeight + gap, W: bounds.W, H: maxInt(0, bounds.H-topHeight-gap)}
}

func verifyFixtureWorkerWake(ctx *Context) error {
	if err := ctx.Clear(); err != nil {
		return err
	}
	width, _, err := ctx.ScreenSize()
	if err != nil {
		return err
	}
	if err := ctx.DrawTitleIn(Rect{X: 0, Y: 0, W: width, H: ctx.TitleHeight()}, "Itch.io · fixture wake probe"); err != nil {
		return err
	}
	ctx.RequestFrame()
	if err := ctx.Present(); err != nil {
		return err
	}
	for {
		_, ok, err := ctx.PollInput()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = ctx.Wake()
	}()
	started := time.Now()
	if err := ctx.Present(); err != nil {
		return err
	}
	if elapsed := time.Since(started); elapsed > 750*time.Millisecond {
		return fmt.Errorf("Catastrophe worker wake took %s", elapsed)
	}
	wakeSeen := false
	for {
		event, ok, err := ctx.PollInput()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		wakeSeen = wakeSeen || event.Wake
	}
	if !wakeSeen {
		return errors.New("Catastrophe worker wake event was not delivered")
	}
	return nil
}

// verifyFixtureInputFrames checks Present's needs_frame handling against
// Catastrophe's own input queue. Once a frame began after the last event and
// the queue is empty, Present waits for the next event instead of returning
// at once for an identical frame. Input not drawn yet, input still queued and
// a requested frame keep the next frame coming at once.
func verifyFixtureInputFrames(ctx *Context) error {
	drain := func() (int, error) {
		taken := 0
		for {
			event, ok, err := ctx.PollInput()
			if err != nil || !ok {
				return taken, err
			}
			if !event.Wake {
				taken++
			}
		}
	}
	tap := func() error {
		ctx.fixtureQueueButton(ButtonA, true)
		ctx.fixtureQueueButton(ButtonA, false)
		taken, err := drain()
		if err == nil && taken != 2 {
			err = fmt.Errorf("Catastrophe delivered %d of 2 queued fixture events", taken)
		}
		return err
	}
	settled := func() bool {
		ctx.fixtureSettleInputFrame()
		return !ctx.fixtureFramePending()
	}

	if err := tap(); err != nil {
		return err
	}
	if settled() {
		return errors.New("input the app has not drawn yet lost its frame")
	}
	if err := ctx.Clear(); err != nil {
		return err
	}
	if !settled() {
		return errors.New("a frame that already shows the input asked for another")
	}

	ctx.fixtureQueueButton(ButtonA, true)
	if err := ctx.Clear(); err != nil {
		return err
	}
	if settled() {
		return errors.New("input still queued lost its frame")
	}
	ctx.fixtureQueueButton(ButtonA, false)
	if _, err := drain(); err != nil {
		return err
	}
	if err := ctx.Clear(); err != nil {
		return err
	}
	if !settled() {
		return errors.New("a frame that shows the drained input asked for another")
	}

	ctx.RequestFrame()
	if err := ctx.Clear(); err != nil {
		return err
	}
	if settled() {
		return errors.New("a frame the app asked for was dropped")
	}
	if !settled() {
		return errors.New("a requested frame was kept after the present that drew it")
	}
	return nil
}

// verifyFixtureTextCache checks the bridge's text run cache: text drawn or
// measured again in a later frame reuses its run and texture instead of
// rasterizing it again, and a font size change empties the cache, whose runs
// are keyed on fonts that the change replaces.
func verifyFixtureTextCache(ctx *Context) error {
	const probe = "Text cache probe · 日本 · العربية"
	frame := func() error {
		if err := ctx.Clear(); err != nil {
			return err
		}
		_, err := ctx.DrawFallbackText(FontMedium, probe, 0, 0, ctx.ThemeColor(RoleText), 0)
		return err
	}
	if err := frame(); err != nil {
		return err
	}
	runs, textures := ctx.fixtureTextCache()
	if runs == 0 || textures == 0 {
		return fmt.Errorf("drawn text was not cached: %d runs, %d textures", runs, textures)
	}
	for again := 0; again < 3; again++ {
		if err := frame(); err != nil {
			return err
		}
		ctx.MeasureFallbackText(FontMedium, probe)
	}
	if gotRuns, gotTextures := ctx.fixtureTextCache(); gotRuns != runs || gotTextures != textures {
		return fmt.Errorf("drawing the same text again cached %d runs and %d textures, was %d and %d",
			gotRuns, gotTextures, runs, textures)
	}
	bump := ctx.FontBump()
	other := bump + 1
	if bump > 0 {
		other = bump - 1
	}
	if err := ctx.SetFontBump(other); err != nil {
		return err
	}
	cleared, _ := ctx.fixtureTextCache()
	if err := ctx.SetFontBump(bump); err != nil {
		return err
	}
	if cleared != 0 {
		return fmt.Errorf("a font size change kept %d runs drawn with the old fonts", cleared)
	}
	return nil
}

func fixtureImage(index int) image.Image {
	palettes := [][3]color.RGBA{
		{{20, 24, 39, 255}, {236, 107, 94, 255}, {169, 227, 75, 255}},
		{{247, 242, 232, 255}, {101, 186, 103, 255}, {32, 37, 58, 255}},
		{{40, 48, 74, 255}, {252, 206, 80, 255}, {126, 154, 255, 255}},
	}
	palette := palettes[index%len(palettes)]
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			pixel := palette[0]
			if (x+y+index*30)%110 < 45 {
				pixel = palette[1]
			}
			if (x-160)*(x-160)+(y-100)*(y-100) < (38+index*9)*(38+index*9) {
				pixel = palette[2]
			}
			img.SetRGBA(x, y, pixel)
		}
	}
	return img
}

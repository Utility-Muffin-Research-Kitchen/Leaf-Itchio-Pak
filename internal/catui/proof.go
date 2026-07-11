package catui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
	"runtime"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type ProofConfig struct {
	Frames         int
	ScreenshotPath string
}

var proofItems = []struct {
	name string
	meta string
}{
	{"Fresh releases", "Recently published"},
	{"Owned games", "Authenticated library"},
	{"Downloaded", "Ready in Leaf"},
	{"Updates", "Inventory changes"},
	{"Soundtracks", "Both Music roots"},
	{"Content filters", "Local preferences"},
}

func RunProof(config ProofConfig) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctx, err := Init(Config{
		Title:            "Itch.io Catastrophe proof",
		FontPath:         os.Getenv("CAT_FONT_PATH"),
		FallbackFontsDir: os.Getenv("ITCHIO_RES_DIR"),
	})
	if err != nil {
		return err
	}
	defer ctx.Close()

	frames, err := proofAnimatedGIF()
	if err != nil {
		return err
	}
	animated := make([]*Texture, 0, len(frames))
	for _, frame := range frames {
		texture, uploadErr := ctx.TextureFromImage(frame)
		if uploadErr != nil {
			return uploadErr
		}
		animated = append(animated, texture)
	}
	qr, err := ctx.TextureFromImage(proofQRCode())
	if err != nil {
		return err
	}

	wrongThread := make(chan error, 1)
	go func() {
		wrongThread <- ctx.DrawRect(Rect{0, 0, 1, 1}, RGBA(0, 0, 0, 0))
	}()
	if threadErr := <-wrongThread; !errors.Is(threadErr, ErrWrongThread) {
		return fmt.Errorf("Catastrophe owner-thread guard: got %v, want %v", threadErr, ErrWrongThread)
	}

	// Prove that a worker completion wakes an otherwise idle cat_present. This
	// deliberately schedules no animation deadline; MLP1 must return through
	// Catastrophe's generic wake pipe rather than waiting for evdev input.
	if err := ctx.Clear(); err != nil {
		return err
	}
	if err := ctx.DrawTitle("Itch.io · worker wake probe"); err != nil {
		return err
	}
	// Flush initial window/input events and clear Catastrophe's first-frame
	// request so the following present is genuinely idle.
	ctx.RequestFrame()
	if err := ctx.Present(); err != nil {
		return err
	}
	for {
		_, ok, pollErr := ctx.PollInput()
		if pollErr != nil {
			return pollErr
		}
		if !ok {
			break
		}
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = ctx.Wake()
	}()
	wakeStarted := time.Now()
	if err := ctx.Present(); err != nil {
		return err
	}
	if elapsed := time.Since(wakeStarted); elapsed > 750*time.Millisecond {
		return fmt.Errorf("Catastrophe worker wake took %s", elapsed)
	}
	wakeSeen := false
	for {
		event, ok, pollErr := ctx.PollInput()
		if pollErr != nil {
			return pollErr
		}
		if !ok {
			break
		}
		if event.Wake {
			wakeSeen = true
		}
	}
	if !wakeSeen {
		return errors.New("Catastrophe worker wake event was not delivered")
	}

	selected := 0
	running := true
	drawnFrames := 0
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
				wakeSeen = true
				continue
			}
			if !event.Pressed {
				continue
			}
			switch event.Button {
			case ButtonUp:
				selected = (selected + len(proofItems) - 1) % len(proofItems)
			case ButtonDown:
				selected = (selected + 1) % len(proofItems)
			case ButtonB, ButtonMenu, ButtonQuit:
				running = false
			}
		}

		if !running {
			break
		}
		frame := animated[drawnFrames%len(animated)]
		if err := drawProofFrame(ctx, frame, qr, selected, wakeSeen); err != nil {
			return err
		}
		drawnFrames++
		if config.ScreenshotPath != "" && config.Frames > 0 && drawnFrames == config.Frames {
			if err := ctx.ScreenshotPNG(config.ScreenshotPath); err != nil {
				return err
			}
		}
		if config.Frames > 0 && drawnFrames >= config.Frames {
			running = false
		} else {
			ctx.RequestFrameIn(120)
		}
		if !running {
			break
		}
		if err := ctx.Present(); err != nil {
			return err
		}
	}
	return nil
}

func drawProofFrame(ctx *Context, animated, qr *Texture, selected int, wakeSeen bool) error {
	if err := ctx.Clear(); err != nil {
		return err
	}
	width, height, err := ctx.ScreenSize()
	if err != nil {
		return err
	}

	root := NewBox(0, 0, width, height, 0)
	root.CarveTop(ctx.TitleHeight())
	if ctx.HintsEnabled() {
		root.CarveBottom(ctx.FooterHeight())
	}
	if err := ctx.DrawTitle("Itch.io · Leaf preview"); err != nil {
		return err
	}

	pad := ctx.Scale(16)
	root.PadTop += pad
	root.PadRight += pad
	root.PadBottom += pad
	root.PadLeft += pad
	content := root.Content()
	left, right := root.SplitColumns(content.W*58/100, ctx.Scale(18))

	rowBase := ctx.FontHeight(FontMedium) + ctx.Scale(14)
	rowRegion, visibleRows, rowHeight := left.FitRows(rowBase, len(proofItems), 0)
	for row := 0; row < visibleRows && row < len(proofItems); row++ {
		y := rowRegion.Y + row*rowHeight
		rowRect := Rect{rowRegion.X, y, rowRegion.W, rowHeight}
		selectedRow := row == selected
		if selectedRow {
			pill := rowRect
			pill.Y += ctx.Scale(3)
			pill.H -= ctx.Scale(6)
			if err := ctx.DrawPill(pill, ctx.ThemeColor(RoleHighlight)); err != nil {
				return err
			}
		}
		textColor := ctx.ThemeColor(RoleText)
		if selectedRow {
			textColor = ctx.ThemeColor(RoleHighlightedText)
		}
		textY := y + (rowHeight-ctx.FontHeight(FontMedium))/2
		if _, err := ctx.DrawText(FontMedium, proofItems[row].name,
			rowRegion.X+ctx.Scale(12), textY, textColor,
			rowRegion.W-ctx.Scale(24), true); err != nil {
			return err
		}
	}

	rightRect := right.Content()
	if err := ctx.SetClip(rightRect); err != nil {
		return err
	}

	artSize := rightRect.W * 52 / 100
	if artSize > rightRect.H/2 {
		artSize = rightRect.H / 2
	}
	art := Rect{rightRect.X, rightRect.Y, artSize, artSize}
	if err := animated.Draw(art); err != nil {
		return err
	}
	qrSize := artSize * 48 / 100
	qrRect := Rect{rightRect.X + rightRect.W - qrSize, rightRect.Y, qrSize, qrSize}
	if err := qr.Draw(qrRect); err != nil {
		return err
	}

	textY := art.Y + art.H + ctx.Scale(12)
	if _, err := ctx.DrawFallbackText(FontLarge, "葉っぱ · مرحباً · नमस्ते",
		rightRect.X, textY, ctx.ThemeColor(RoleEmphasis), rightRect.W); err != nil {
		return err
	}
	textY += ctx.FontHeight(FontLarge) + ctx.Scale(8)
	if _, err := ctx.DrawText(FontSmall, proofItems[selected].meta,
		rightRect.X, textY, ctx.ThemeColor(RoleHint), rightRect.W, true); err != nil {
		return err
	}
	textY += ctx.FontHeight(FontSmall) + ctx.Scale(12)

	progress := float32(selected+1) / float32(len(proofItems))
	progressRect := Rect{rightRect.X, textY, rightRect.W - ctx.Scale(28), ctx.Scale(8)}
	if err := ctx.DrawProgress(progressRect, progress, ctx.ThemeColor(RoleHighlight), ctx.ThemeColor(RoleDisabled)); err != nil {
		return err
	}
	triangle := Rect{progressRect.X + progressRect.W + ctx.Scale(8), textY - ctx.Scale(3), ctx.Scale(12), ctx.Scale(14)}
	if err := ctx.DrawTriangle(triangle, DirectionRight, ctx.ThemeColor(RoleEmphasis)); err != nil {
		return err
	}

	wakeLabel := "no"
	if wakeSeen {
		wakeLabel = "yes"
	}
	status := fmt.Sprintf("58/42 · r%d · b%d · wake %s", visibleRows, ctx.FontBump(), wakeLabel)
	textY += ctx.Scale(18)
	if _, err := ctx.DrawText(FontTiny, status, rightRect.X, textY,
		ctx.ThemeColor(RoleHint), rightRect.W, true); err != nil {
		return err
	}
	if err := ctx.ResetClip(); err != nil {
		return err
	}

	if ctx.HintsEnabled() {
		if err := ctx.DrawFooter([]FooterItem{
			{Button: ButtonB, Label: "Exit"},
			{Button: ButtonA, Label: "Open", IsConfirm: true},
		}); err != nil {
			return err
		}
	}
	return nil
}

func proofAnimatedGIF() ([]image.Image, error) {
	palette := color.Palette{
		color.RGBA{20, 24, 39, 255},
		color.RGBA{236, 107, 94, 255},
		color.RGBA{169, 227, 75, 255},
		color.RGBA{247, 242, 232, 255},
	}
	animation := &gif.GIF{LoopCount: 0}
	for frame := 0; frame < 3; frame++ {
		img := image.NewPaletted(image.Rect(0, 0, 160, 160), palette)
		for y := 0; y < 160; y++ {
			for x := 0; x < 160; x++ {
				index := uint8(0)
				if (x+y+frame*22)%90 < 42 {
					index = 1
				}
				if (x-frame*24-80)*(x-frame*24-80)+(y-80)*(y-80) < 36*36 {
					index = 2
				}
				if x > 18 && x < 142 && y > 112 && y < 138 {
					index = 3
				}
				img.SetColorIndex(x, y, index)
			}
		}
		animation.Image = append(animation.Image, img)
		animation.Delay = append(animation.Delay, 12)
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		return nil, err
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		return nil, err
	}
	frames := make([]image.Image, 0, len(decoded.Image))
	for _, source := range decoded.Image {
		rgba := image.NewRGBA(source.Bounds())
		draw.Draw(rgba, rgba.Bounds(), source, source.Bounds().Min, draw.Src)
		frames = append(frames, rgba)
	}
	return frames, nil
}

func proofQRCode() image.Image {
	code, err := qrcode.New("https://github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak", qrcode.Medium)
	if err != nil {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	bitmap := code.Bitmap()
	size := len(bitmap)
	image := image.NewRGBA(image.Rect(0, 0, size, size))
	for y, row := range bitmap {
		for x, set := range row {
			pixel := color.RGBA{247, 242, 232, 255}
			if set {
				pixel = color.RGBA{20, 24, 39, 255}
			}
			image.SetRGBA(x, y, pixel)
		}
	}
	return image
}

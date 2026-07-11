package catui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/skip2/go-qrcode"
)

type DetailScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.DetailModel
	cache *ImageCache
	qr    *Texture
}

func NewDetailScreen(ctx *Context, model *appui.DetailModel, cache *ImageCache) (*DetailScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	screen := &DetailScreen{ctx: ctx, ui: ui, model: model, cache: cache}
	if model.Game.URL != "" {
		if code, qrErr := qrcode.New(model.Game.URL, qrcode.Medium); qrErr == nil {
			screen.qr, _ = ctx.TextureFromImage(code.Image(256))
		}
	}
	return screen, nil
}

func (screen *DetailScreen) Close() {
	if screen.qr != nil {
		_ = screen.qr.Destroy()
		screen.qr = nil
	}
}

func (screen *DetailScreen) HandleInput(event InputEvent) appui.DetailIntent {
	if event.Wake {
		return appui.DetailIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *DetailScreen) Draw() error {
	screen.cache.BeginFrame()
	footer := []FooterHint{{Button: ButtonB, Label: "Back"}}
	if screen.model.State == appui.DetailReady {
		footer = append(footer,
			FooterHint{Button: ButtonL1, Label: "Image -/+", NarrowLabel: "Images"},
			FooterHint{Button: ButtonUp, Label: "Scroll"})
		if screen.model.Game.CanDownload && !screen.model.BrowserOnly {
			label := "Download"
			if screen.model.Game.Downloaded {
				label = "Again"
			}
			// Keep this with the left group. Splitting a GIF-backed Detail frame
			// across Cat's left/right footer groups can retain queued shared-sprite
			// state on MLP1; the action and visual label remain unchanged.
			footer = append(footer, FooterHint{Button: ButtonA, Label: label})
		}
		if screen.model.Game.Downloaded {
			footer = append(footer, FooterHint{Button: ButtonX, Label: "Manage", NarrowLabel: "Files"})
		}
	} else if screen.model.State == appui.DetailWarning {
		footer = append(footer, FooterHint{Button: ButtonStart, Label: "Settings", NarrowLabel: "Set"})
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title:           screen.model.Game.Title,
		SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10),
		Footer:          footer,
	})
	if err != nil {
		return err
	}
	if err := screen.ui.DrawSubHeader(frame.Layout.SubHeader, screen.subtitle()); err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	switch screen.model.State {
	case appui.DetailLoading:
		err = screen.ui.DrawState(body, StateLoading, "Loading game details", "Reading screenshots, tags, and download metadata…")
	case appui.DetailError:
		detail := screen.model.ErrorDetail
		if detail == "" {
			detail = "The itch.io page could not be loaded."
		}
		err = screen.ui.DrawState(body, StateError, "Could not load details", detail)
	case appui.DetailWarning:
		err = screen.ui.DrawWarningCover(body, "Content warning",
			"This game matches one or more enabled content filters. Return to the list, or review the filter settings before continuing.")
	default:
		err = screen.drawReady(frame.Layout.Content)
	}
	if err != nil {
		return err
	}
	return frame.Finish()
}

func (screen *DetailScreen) subtitle() string {
	parts := make([]string, 0, 4)
	if screen.model.Game.Author != "" {
		parts = append(parts, "by "+screen.model.Game.Author)
	}
	if screen.model.Game.Platform != "" {
		parts = append(parts, screen.model.Game.Platform)
	}
	switch {
	case screen.model.Game.Downloaded:
		parts = append(parts, "Downloaded")
	case screen.model.BrowserOnly:
		parts = append(parts, "Browser-only")
	case screen.model.Game.IsFree:
		parts = append(parts, "Free")
	default:
		parts = append(parts, "$"+strconv.FormatFloat(screen.model.Game.Price, 'f', 2, 64))
	}
	return strings.Join(parts, "  ·  ")
}

func (screen *DetailScreen) drawReady(content Box) error {
	split := ListDetailSplit(content, 60, screen.ui.BasePadding)
	gallery := split.List.Content()
	labelHeight := screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(8)
	imageArea := Rect{X: gallery.X, Y: gallery.Y, W: gallery.W, H: maxInt(0, gallery.H-labelHeight)}
	if len(screen.model.Images) == 0 {
		if err := screen.ui.DrawState(imageArea, StateEmpty, "No screenshots", "Scan the QR code to view the itch.io page."); err != nil {
			return err
		}
	} else {
		index := screen.model.ImageIndex
		key := screen.model.Images[index]
		if texture := screen.cache.Get(key); texture != nil {
			if err := screen.ui.DrawImageFit(texture, imageArea); err != nil {
				return err
			}
		} else if screen.cache.Failed(key) {
			if err := screen.ui.DrawState(imageArea, StateError, "No image", "Artwork could not be decoded."); err != nil {
				return err
			}
		} else if err := screen.ui.DrawState(imageArea, StateLoading, "Loading image", ""); err != nil {
			return err
		}
		// Warm only adjacent images. This preserves GIF support without decoding
		// an entire animated gallery at once on constrained devices.
		if len(screen.model.Images) > 1 {
			screen.cache.Warm(screen.model.Images[(index+1)%len(screen.model.Images)])
		}
		label := fmt.Sprintf("Image %d/%d", index+1, len(screen.model.Images))
		y := gallery.Y + gallery.H - labelHeight + screen.ctx.Scale(3)
		_, err := screen.ctx.DrawText(FontSmall, label, gallery.X, y,
			screen.ctx.ThemeColor(RoleHint), gallery.W, true)
		if err != nil {
			return err
		}
	}

	panel := split.Detail.Content()
	qrSize := minInt(panel.W, maxInt(screen.ctx.Scale(86), panel.H/4))
	qrRect := Rect{X: panel.X + (panel.W-qrSize)/2, Y: panel.Y, W: qrSize, H: qrSize}
	if screen.qr != nil {
		if err := screen.qr.Draw(qrRect); err != nil {
			return err
		}
	}
	y := qrRect.Y + qrRect.H + screen.ctx.Scale(5)
	if _, err := screen.ctx.DrawText(FontTiny, "Scan to open on itch.io", panel.X, y,
		screen.ctx.ThemeColor(RoleHint), panel.W, true); err != nil {
		return err
	}
	y += screen.ctx.FontHeight(FontTiny) + screen.ui.BasePadding/2

	tagsHeight := minInt(screen.ctx.Scale(78), maxInt(0, panel.Y+panel.H-y))
	used, err := screen.ui.DrawTagPills(Rect{X: panel.X, Y: y, W: panel.W, H: tagsHeight}, screen.model.Tags)
	if err != nil {
		return err
	}
	y += used
	if used > 0 {
		y += screen.ui.BasePadding / 2
	}
	description := Rect{X: panel.X, Y: y, W: panel.W, H: maxInt(0, panel.Y+panel.H-y)}
	lineHeight := screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(5)
	totalLines := 0
	for _, paragraph := range screen.model.Description {
		totalLines += len(wrapText(paragraph, description.W, func(value string) int {
			return screen.ctx.MeasureText(FontSmall, value)
		})) + 1
	}
	visible := 1
	if lineHeight > 0 {
		visible = maxInt(1, description.H/lineHeight)
	}
	screen.model.SetScrollBounds(maxInt(0, totalLines-visible))
	paragraphs := screen.model.Description
	if len(paragraphs) == 0 {
		paragraphs = []string{"No description was provided."}
	}
	return screen.ui.DrawScrollingBody(description, "About", paragraphs, screen.model.ScrollLine)
}

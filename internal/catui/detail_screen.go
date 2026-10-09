package catui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
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
	screen.qr = newQRTexture(ctx, model.Game.URL)
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

// detailFooter lists the Detail hints. B Back is always shown. A downloaded
// game needs five hints, which do not fit the 960-wide MLP1 display at
// larger font bumps, so Settings and then the image hint carry a DropRank:
// the composer leaves them out before Cat would collapse the footer into a
// synthetic +1 item. Start and L1/R1 keep working when their hint is hidden.
func detailFooter(model *appui.DetailModel) []FooterHint {
	footer := []FooterHint{{Button: ButtonB, Label: "Back"}}
	if model.State == appui.DetailReady {
		// D-pad scrolling has no hint in this width-constrained footer, so
		// Settings stays visible: a description that does not fit shows the
		// scrollbar instead.
		footer = append(footer, FooterHint{Button: ButtonL1, ButtonText: "L1/R1", Label: "Img.", DropRank: 2})
		if model.Game.CanDownload && !model.BrowserOnly {
			label := "Download"
			if model.Game.Downloaded {
				label = "Again"
			}
			// Keep this with the left group. Splitting a GIF-backed Detail frame
			// across Cat's left/right footer groups can retain queued shared-sprite
			// state on MLP1; the action and visual label remain unchanged.
			footer = append(footer, FooterHint{Button: ButtonA, Label: label})
		} else if model.Game.NeedsSignIn && !model.BrowserOnly {
			footer = append(footer, FooterHint{Button: ButtonA, Label: "Sign in"})
		}
	}
	if (model.State == appui.DetailReady || model.State == appui.DetailError) && model.Game.Downloaded {
		footer = append(footer, FooterHint{Button: ButtonX, Label: "Manage"})
	}
	return append(footer, FooterHint{Button: ButtonStart, ButtonText: "STR", Label: "Settings", NarrowLabel: "Set", DropRank: 1})
}

func (screen *DetailScreen) Draw() error {
	screen.cache.BeginFrame()
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title:           screen.model.Game.Title,
		SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10),
		Footer:          detailFooter(screen.model),
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
	case appui.DetailWarning:
		err = screen.ui.DrawWarningCover(body, "Content warning", appui.WarningText(screen.model.WarningCategories))
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
	price := screen.model.Game.PriceText()
	switch {
	case screen.model.Game.Downloaded:
		parts = append(parts, "Downloaded")
		if price != "" {
			parts = append(parts, price)
		}
	case screen.model.BrowserOnly:
		parts = append(parts, "Browser-only")
	case price != "":
		parts = append(parts, price)
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
		title := "No screenshots"
		if screen.model.State == appui.DetailError {
			title = "No cached image"
		}
		if err := screen.ui.DrawState(imageArea, StateEmpty, title, "Scan the QR code to view the itch.io page."); err != nil {
			return err
		}
	} else {
		index := screen.model.ImageIndex
		key := screen.model.Images[index]
		texture := screen.cache.Peek(key)
		if texture == nil && screen.model.State != appui.DetailError {
			texture = screen.cache.Get(key)
		}
		if texture != nil {
			if err := screen.ui.DrawImageFit(texture, imageArea); err != nil {
				return err
			}
		} else if screen.model.State == appui.DetailError {
			if err := screen.ui.DrawState(imageArea, StateEmpty, "No cached image", ""); err != nil {
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
		if len(screen.model.Images) > 1 && screen.model.State != appui.DetailError {
			screen.cache.Warm(screen.model.Images[(index+1)%len(screen.model.Images)])
		}
		label := fmt.Sprintf("Image %d/%d", index+1, len(screen.model.Images))
		if screen.model.State == appui.DetailError {
			label = ""
			if texture != nil {
				label = "Cached cover"
			}
		}
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
	caption := "Scan to open on itch.io"
	if screen.model.Game.NotOwned() && !screen.model.BrowserOnly {
		caption = "Not owned. Scan to buy."
	}
	y, err := screen.ui.DrawQRCaption(qrRect, panel.X, panel.W, caption)
	if err != nil {
		return err
	}
	y += screen.ui.BasePadding / 2

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
	heading := "About"
	paragraphs := screen.model.Description
	if screen.model.State == appui.DetailError {
		heading = "Game page unavailable"
		paragraphs = []string{unavailableText(screen.model.ErrorDetail, screen.model.Game.Downloaded)}
	} else if len(paragraphs) == 0 {
		paragraphs = []string{"No description was provided."}
	}
	// The scrollbar beside a description that does not fit is the scroll
	// hint the footer leaves out.
	return screen.ui.DrawScrollingBody(description, heading, paragraphs, &screen.model.BodyScroll)
}

// unavailableText follows the reason the game page is unavailable with what
// you can still do on this screen.
func unavailableText(reason string, downloaded bool) string {
	if downloaded {
		return reason + " You can still manage your files."
	}
	return reason
}

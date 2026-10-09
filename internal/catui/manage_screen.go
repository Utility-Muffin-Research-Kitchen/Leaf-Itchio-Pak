package catui

import (
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

type ManageScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.ManageModel
}

func NewManageScreen(ctx *Context, model *appui.ManageModel) (*ManageScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &ManageScreen{ctx: ctx, ui: ui, model: model}, nil
}

func (screen *ManageScreen) HandleInput(event InputEvent) appui.ManageIntent {
	if event.Wake {
		return appui.ManageIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *ManageScreen) Draw() error {
	footer := []FooterHint{{Button: ButtonB, Label: "Back"}}
	switch screen.model.State {
	case appui.ManageList:
		footer = append(footer, FooterHint{Button: ButtonA, Label: "Select", IsConfirm: true})
	case appui.ManageConfirm:
		footer = []FooterHint{
			{Button: ButtonB, Label: "Cancel"},
			{Button: ButtonA, Label: "Confirm", IsConfirm: true},
		}
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title: screen.model.Title, SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10),
		Footer: footer,
	})
	if err != nil {
		return err
	}
	subtitle := screen.model.Subtitle
	if screen.model.State == appui.ManageConfirm {
		subtitle = "Confirm filesystem change"
	}
	if err := screen.ui.DrawSubHeader(frame.Layout.SubHeader, subtitle); err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	switch screen.model.State {
	case appui.ManageConfirm:
		err = screen.ui.DrawScrollingBody(body, screen.model.PromptTitle, screen.model.PromptLines, &screen.model.BodyScroll)
	case appui.ManageResult:
		lines := []string{screen.model.Message}
		if screen.model.LibraryStatus != "" {
			lines = append(lines, screen.model.LibraryStatus)
		}
		err = screen.ui.DrawScrollingBody(body, "Changes complete", lines, &screen.model.BodyScroll)
	case appui.ManageError:
		err = screen.ui.DrawState(body, StateError, "Could not continue", screen.model.Message)
	default:
		err = screen.drawList(frame.Layout.Content)
	}
	if err != nil {
		return err
	}
	return frame.Finish()
}

func (screen *ManageScreen) drawList(box Box) error {
	rowHeight := screen.ctx.FontHeight(FontMedium) + screen.ctx.Scale(18)
	for _, item := range screen.model.Items {
		if item.Note != "" {
			// Every row makes room for a note line, so the list keeps one
			// pitch; the rows' own padding shrinks a little to fit it.
			rowHeight += screen.ctx.FontHeight(FontTiny) - screen.ctx.Scale(8)
			break
		}
	}
	geometry := FitScrollingList(box, rowHeight, len(screen.model.Items), 0)
	screen.model.VisibleRows = geometry.VisibleRows
	start := screen.model.Cursor - geometry.VisibleRows + 1
	if start < 0 {
		start = 0
	}
	for row := 0; row < geometry.VisibleRows && start+row < len(screen.model.Items); row++ {
		index := start + row
		item := screen.model.Items[index]
		secondary := item.Badge
		if secondary == "" {
			secondary = item.Detail
		}
		selected := index == screen.model.Cursor
		var err error
		if item.Note != "" {
			err = screen.drawNotedRow(geometry.Row(row), item.Label, item.Note, secondary, selected)
		} else {
			err = screen.ui.DrawListRow(geometry.Row(row), item.Label, secondary, selected)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// drawNotedRow draws a row like DrawListRow, with note as a line of small
// text under the label, such as the archive member a file came from.
func (screen *ManageScreen) drawNotedRow(rect Rect, label, note, secondary string, selected bool) error {
	ctx := screen.ctx
	if rect.W <= 0 || rect.H <= 0 {
		return nil
	}
	if selected {
		pill := insetRect(rect, 0, ctx.Scale(3))
		pill.W -= ctx.Scale(4)
		if err := ctx.DrawPill(pill, ctx.ThemeColor(RoleHighlight)); err != nil {
			return err
		}
	}
	return screen.ui.withClip(rect, func() error {
		// The same colors as DrawListRow; the note takes the secondary
		// text's color, which is the row's text color on the highlight.
		labelRole, secondaryRole := listRowRoles(selected)
		labelColor, secondaryColor := ctx.ThemeColor(labelRole), ctx.ThemeColor(secondaryRole)
		noteColor := secondaryColor
		pad := ctx.Scale(12)
		maxWidth := rect.W - pad*2
		if secondary != "" {
			secondaryWidth := minInt(ctx.MeasureText(FontTiny, secondary), maxInt(0, rect.W/3))
			maxWidth -= secondaryWidth + pad
			if secondaryWidth > 0 {
				if _, err := ctx.DrawText(FontTiny, secondary, rect.X+rect.W-pad-secondaryWidth,
					rect.Y+(rect.H-ctx.FontHeight(FontTiny))/2, secondaryColor, secondaryWidth, true); err != nil {
					return err
				}
			}
		}
		if maxWidth <= 0 {
			return nil
		}
		labelHeight, gap := ctx.FontHeight(FontMedium), ctx.Scale(2)
		y := rect.Y + (rect.H-labelHeight-gap-ctx.FontHeight(FontTiny))/2
		if _, err := screen.ui.DrawEllipsizedText(FontMedium, label, rect.X+pad, y, labelColor, maxWidth); err != nil {
			return err
		}
		// The fallback fonts draw a member name in any script.
		measure := func(text string) int { return ctx.MeasureFallbackText(FontTiny, text) }
		_, err := ctx.DrawFallbackText(FontTiny, fitNoteText(note, maxWidth, measure),
			rect.X+pad, y+labelHeight+gap, noteColor, maxWidth)
		return err
	})
}

// fitNoteText cuts text to fit width with "...", as Catastrophe's ellipsis
// does, cutting between characters rather than bytes.
func fitNoteText(text string, width int, measure func(string) int) string {
	if measure(text) <= width {
		return text
	}
	runes := []rune(text)
	cut := func(n int) string { return strings.TrimRight(string(runes[:n]), " ") + "..." }
	low, high := 0, len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		if measure(cut(mid)) <= width {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return cut(low)
}

package catui

import "github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"

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
		err = screen.ui.DrawScrollingBody(body, screen.model.PromptTitle, screen.model.PromptLines, 0)
	case appui.ManageResult:
		lines := []string{screen.model.Message}
		if screen.model.LibraryStatus != "" {
			lines = append(lines, screen.model.LibraryStatus)
		}
		err = screen.ui.DrawScrollingBody(body, "Changes complete", lines, 0)
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
	geometry := FitScrollingList(box,
		screen.ctx.FontHeight(FontMedium)+screen.ctx.Scale(18), len(screen.model.Items), 0)
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
		if err := screen.ui.DrawListRow(geometry.Row(row), item.Label, secondary,
			index == screen.model.Cursor); err != nil {
			return err
		}
	}
	return nil
}

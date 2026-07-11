package catui

import "github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"

type DestinationScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.DestinationModel
}

func NewDestinationScreen(ctx *Context, model *appui.DestinationModel) (*DestinationScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &DestinationScreen{ctx: ctx, ui: ui, model: model}, nil
}

func (screen *DestinationScreen) HandleInput(event InputEvent) appui.DestinationIntent {
	if event.Wake {
		return appui.DestinationIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *DestinationScreen) Draw() error {
	footer := []FooterHint{{Button: ButtonB, Label: "Back"}}
	if screen.model.Phase != appui.DestinationError {
		label := "Select"
		if screen.model.Phase == appui.DestinationFolders && screen.model.Cursor >= 0 &&
			screen.model.Cursor < len(screen.model.Items) &&
			screen.model.Items[screen.model.Cursor].Kind == appui.DestinationItemSave {
			label = "Save here"
		}
		footer = append(footer, FooterHint{Button: ButtonA, Label: label, NarrowLabel: "Select"})
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title: screen.model.Title, SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10),
		Footer: footer,
	})
	if err != nil {
		return err
	}
	subtitle := screen.model.Subtitle
	if screen.model.Path != "" {
		subtitle += "  ·  " + screen.model.Path
	}
	if err := screen.ui.DrawSubHeader(frame.Layout.SubHeader, subtitle); err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	if screen.model.Phase == appui.DestinationError {
		if err := screen.ui.DrawScrollingBody(body, "Destination unavailable",
			[]string{screen.model.ErrorDetail}, 0); err != nil {
			return err
		}
		return frame.Finish()
	}
	geometry := FitScrollingList(frame.Layout.Content,
		screen.ctx.FontHeight(FontMedium)+screen.ctx.Scale(18), len(screen.model.Items), 0)
	screen.model.VisibleRows = geometry.VisibleRows
	start := screen.model.Cursor - geometry.VisibleRows + 1
	if start < 0 {
		start = 0
	}
	for row := 0; row < geometry.VisibleRows && start+row < len(screen.model.Items); row++ {
		index := start + row
		item := screen.model.Items[index]
		primary := item.Label
		secondary := item.Detail
		switch item.Kind {
		case appui.DestinationItemSave:
			secondary = "SAVE"
		case appui.DestinationItemUp:
			primary, secondary = "..", "UP"
		}
		if err := screen.ui.DrawListRow(geometry.Row(row), primary, secondary,
			index == screen.model.Cursor); err != nil {
			return err
		}
	}
	return frame.Finish()
}

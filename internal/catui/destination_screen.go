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

// destinationFooter lists the hints of every destination screen: the card
// picker, the folder picker, the confirmation and the unavailable message.
// B Back stays in the left group. A, which selects or confirms, is in the
// right group, as it is on the Manage list, Settings and the download picker;
// the unavailable message has no A hint.
func destinationFooter(model *appui.DestinationModel) []FooterHint {
	footer := []FooterHint{{Button: ButtonB, Label: "Back"}}
	if model.Phase == appui.DestinationError {
		return footer
	}
	label := "Select"
	if model.Phase == appui.DestinationConfirm {
		label = "Download"
	} else if model.Phase == appui.DestinationFolders && model.Cursor >= 0 &&
		model.Cursor < len(model.Items) && model.Items[model.Cursor].Kind == appui.DestinationItemSave {
		label = "Save here"
	}
	return append(footer, FooterHint{Button: ButtonA, Label: label, NarrowLabel: "Select", IsConfirm: true})
}

func (screen *DestinationScreen) Draw() error {
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title: screen.model.Title, SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10),
		Footer: destinationFooter(screen.model),
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
			[]string{screen.model.ErrorDetail}, &screen.model.BodyScroll); err != nil {
			return err
		}
		return frame.Finish()
	}
	if screen.model.Phase == appui.DestinationConfirm {
		if err := screen.ui.DrawScrollingBlocks(body, "Download to this location?",
			screen.model.Summary, &screen.model.BodyScroll); err != nil {
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

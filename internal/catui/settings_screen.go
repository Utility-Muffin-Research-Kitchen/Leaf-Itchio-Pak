package catui

import "github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"

type SettingsScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.SettingsModel
}

func NewSettingsScreen(ctx *Context, model *appui.SettingsModel) (*SettingsScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &SettingsScreen{ctx: ctx, ui: ui, model: model}, nil
}

func (screen *SettingsScreen) HandleInput(event InputEvent) appui.SettingsIntent {
	if event.Wake {
		return appui.SettingsIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *SettingsScreen) Draw() error {
	footer := []FooterHint{{Button: ButtonB, Label: "Back"}}
	if screen.model.State == appui.SettingsList {
		if row, ok := screen.model.Selected(); ok && row.ActionEnabled {
			footer = append(footer, FooterHint{Button: ButtonA, Label: "Select", IsConfirm: true})
		}
	} else if screen.model.State == appui.SettingsConfirm {
		footer = []FooterHint{{Button: ButtonB, Label: "Cancel"}, {Button: ButtonA, Label: "Confirm", IsConfirm: true}}
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title: screen.model.Title, SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10), Footer: footer,
	})
	if err != nil {
		return err
	}
	subtitle := screen.model.Subtitle
	if screen.model.State == appui.SettingsConfirm {
		subtitle = "Confirm settings change"
	}
	if err := screen.ui.DrawSubHeader(frame.Layout.SubHeader, subtitle); err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	switch screen.model.State {
	case appui.SettingsConfirm:
		err = screen.ui.DrawScrollingBody(body, screen.model.PromptTitle, screen.model.PromptLines, 0)
	case appui.SettingsMessage:
		err = screen.ui.DrawState(body, StateEmpty, "Settings updated", screen.model.Message)
	case appui.SettingsError:
		err = screen.ui.DrawState(body, StateError, "Could not update settings", screen.model.Message)
	case appui.SettingsWorking:
		err = screen.ui.DrawState(body, StateLoading, "Working", screen.model.Message)
	default:
		err = screen.drawRows(frame.Layout.Content)
	}
	if err != nil {
		return err
	}
	return frame.Finish()
}

func (screen *SettingsScreen) drawRows(box Box) error {
	geometry := FitScrollingList(box, screen.ctx.FontHeight(FontMedium)+screen.ctx.Scale(18), len(screen.model.Rows), 0)
	screen.model.VisibleRows = geometry.VisibleRows
	start := screen.model.Cursor - geometry.VisibleRows + 1
	if start < 0 {
		start = 0
	}
	for row := 0; row < geometry.VisibleRows && start+row < len(screen.model.Rows); row++ {
		index := start + row
		item := screen.model.Rows[index]
		if err := screen.ui.DrawValueRow(geometry.Row(row), item.Label, item.Value,
			index == screen.model.Cursor, false); err != nil {
			return err
		}
	}
	return nil
}

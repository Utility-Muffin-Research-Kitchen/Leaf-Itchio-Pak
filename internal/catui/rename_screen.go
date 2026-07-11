package catui

import "github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"

type RenameScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.RenameModel
}

func NewRenameScreen(ctx *Context, model *appui.RenameModel) (*RenameScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &RenameScreen{ctx: ctx, ui: ui, model: model}, nil
}

func (screen *RenameScreen) HandleInput(event InputEvent) appui.RenameIntent {
	if event.Wake {
		return appui.RenameIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *RenameScreen) Draw() error {
	footer := []FooterHint{{Button: ButtonB, Label: "Cancel"}, {Button: ButtonA, Label: "Rename", IsConfirm: true}}
	if screen.model.State == appui.RenameConfirmSaves || screen.model.State == appui.RenameConfirmStates {
		footer[0].Label = "Skip"
	}
	if screen.model.State == appui.RenameDone || screen.model.State == appui.RenameError {
		footer = []FooterHint{{Button: ButtonB, Label: "Back"}}
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title: screen.model.Title, SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10),
		Footer: footer,
	})
	if err != nil {
		return err
	}
	if err := screen.ui.DrawSubHeader(frame.Layout.SubHeader, screen.model.Subtitle); err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	switch screen.model.State {
	case appui.RenameDone:
		err = screen.ui.DrawState(body, StateEmpty, "Rename complete", screen.model.Message)
	case appui.RenameError:
		err = screen.ui.DrawState(body, StateError, "Rename failed", screen.model.Message)
	default:
		lines := screen.model.Lines
		if len(lines) == 0 {
			lines = []string{"No related files were found."}
		}
		err = screen.ui.DrawScrollingBody(body, screen.model.Heading, lines, screen.model.ScrollLine)
		lineHeight := screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(5)
		visible := 1
		if lineHeight > 0 {
			visible = maxInt(1, body.H/lineHeight-2)
		}
		screen.model.SetScrollBounds(maxInt(0, len(lines)-visible))
	}
	if err != nil {
		return err
	}
	return frame.Finish()
}

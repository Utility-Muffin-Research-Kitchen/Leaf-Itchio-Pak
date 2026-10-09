package catui

import (
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/text"
)

type RefreshScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.RefreshModel
}

func NewRefreshScreen(ctx *Context, model *appui.RefreshModel) (*RefreshScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &RefreshScreen{ctx: ctx, ui: ui, model: model}, nil
}

func (screen *RefreshScreen) HandleInput(event InputEvent) appui.RefreshIntent {
	if event.Wake {
		return appui.RefreshIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

// refreshDoneText says how many games the finished refresh saved.
func refreshDoneText(total int) string {
	return "Saved " + text.Count(total, "game", "games") + " to the local cache."
}

// refreshProgressText says how many games the running refresh has fetched.
func refreshProgressText(fetched int) string {
	return text.Count(fetched, "game", "games") + " fetched · B cancels safely"
}

func (screen *RefreshScreen) Draw() error {
	footer := []FooterHint{{Button: ButtonB, Label: "Cancel"}}
	if screen.model.State != appui.RefreshLoading {
		footer = []FooterHint{{Button: ButtonB, Label: "Back"}}
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{Title: screen.model.Title, Footer: footer})
	if err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	switch screen.model.State {
	case appui.RefreshDone:
		err = screen.ui.DrawState(body, StateEmpty, "Game list updated", refreshDoneText(screen.model.Total))
	case appui.RefreshError:
		err = screen.ui.DrawState(body, StateError, "Refresh failed", screen.model.Detail)
	case appui.RefreshCancelled:
		err = screen.ui.DrawState(body, StateEmpty, "Refresh cancelled", "No partial catalogue was saved.")
	default:
		err = screen.ui.DrawState(body, StateLoading, "Fetching game list", refreshProgressText(screen.model.Fetched))
	}
	if err != nil {
		return err
	}
	return frame.Finish()
}

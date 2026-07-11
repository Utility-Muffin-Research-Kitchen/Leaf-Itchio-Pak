package catui

// WaitScreen is a non-interactive Catastrophe state used while the application
// drains protected background work before sleep or shutdown.
type WaitScreen struct {
	ui       *Composer
	title    string
	headline string
	detail   string
}

func NewWaitScreen(ctx *Context, title, headline, detail string) (*WaitScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &WaitScreen{ui: ui, title: title, headline: headline, detail: detail}, nil
}

func (screen *WaitScreen) Draw() error {
	frame, err := screen.ui.BeginScreen(ScreenSpec{Title: screen.title})
	if err != nil {
		return err
	}
	if err := screen.ui.DrawState(frame.Layout.Content.Content(), StateLoading, screen.headline, screen.detail); err != nil {
		return err
	}
	return frame.Finish()
}

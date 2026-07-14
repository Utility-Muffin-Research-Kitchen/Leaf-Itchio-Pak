package catui

import "github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"

type FilterScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.FilterModel
}

func NewFilterScreen(ctx *Context, model *appui.FilterModel) (*FilterScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &FilterScreen{ctx: ctx, ui: ui, model: model}, nil
}

func (screen *FilterScreen) HandleInput(event InputEvent) appui.FilterIntent {
	if event.Wake {
		return appui.FilterIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *FilterScreen) Draw() error {
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title:           "Filter & Search",
		SubHeaderHeight: screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10),
		Footer: []FooterHint{
			{Button: ButtonB, Label: "Cancel"},
			{Button: ButtonY, Label: "Clear"},
			{Button: ButtonA, Label: "Edit / change", NarrowLabel: "Edit"},
			{Button: ButtonSelect, Label: "Apply", IsConfirm: true},
		},
	})
	if err != nil {
		return err
	}
	if err := screen.ui.DrawSubHeader(frame.Layout.SubHeader, "Changes are staged until SELECT applies them"); err != nil {
		return err
	}
	body := FullWidthBody(frame.Layout.Content)
	geometry := FitScrollingList(NewBox(body.X, body.Y, body.W, body.H, 0),
		screen.ctx.FontHeight(FontMedium)+screen.ctx.Scale(24), 3, 3)
	query := screen.model.Query
	if query == "" {
		query = "Any title or author"
	}
	if err := screen.ui.DrawValueRow(geometry.Row(0), "Search", query,
		screen.model.Section == appui.FilterSearch, false); err != nil {
		return err
	}
	if err := screen.ui.DrawValueRow(geometry.Row(1), "Platform", screen.model.PlatformLabel(),
		screen.model.Section == appui.FilterPlatform, true); err != nil {
		return err
	}
	if err := screen.ui.DrawValueRow(geometry.Row(2), "Sort", screen.model.SortLabel(),
		screen.model.Section == appui.FilterSort, true); err != nil {
		return err
	}
	return frame.Finish()
}

package catui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

type DownloadSelectScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.DownloadSelectModel
}

func NewDownloadSelectScreen(ctx *Context, model *appui.DownloadSelectModel) (*DownloadSelectScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &DownloadSelectScreen{ctx: ctx, ui: ui, model: model}, nil
}

func (screen *DownloadSelectScreen) HandleInput(event InputEvent) appui.DownloadSelectIntent {
	if event.Wake {
		return appui.DownloadSelectIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *DownloadSelectScreen) Draw() error {
	footer := []FooterHint{{Button: ButtonB, Label: "Back"}}
	if screen.model.State == appui.DownloadSelectChoices {
		footer = append(footer, FooterHint{Button: ButtonA, Label: "Select", IsConfirm: true})
		if screen.model.Cursor >= 0 && screen.model.Cursor < len(screen.model.Choices) &&
			len(screen.model.Choices[screen.model.Cursor].FormatOptions) > 0 {
			footer = append(footer, FooterHint{Button: ButtonLeft, Label: "Format"})
		}
	}
	subHeaderHeight := 0
	if screen.model.Subtitle != "" {
		subHeaderHeight = screen.ctx.FontHeight(FontSmall) + screen.ctx.Scale(10)
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{
		Title: screen.model.Title, SubHeaderHeight: subHeaderHeight, Footer: footer,
	})
	if err != nil {
		return err
	}
	if screen.model.Subtitle != "" {
		if err := screen.ui.DrawSubHeader(frame.Layout.SubHeader, screen.model.Subtitle); err != nil {
			return err
		}
	}
	body := frame.Layout.Content.Content()
	switch screen.model.State {
	case appui.DownloadSelectLoading:
		err = screen.ui.DrawState(body, StateLoading, "Finding available files", "Contacting itch.io…")
	case appui.DownloadSelectError:
		err = screen.ui.DrawScrollingBody(body, "Could not continue", []string{screen.model.Message}, 0)
	case appui.DownloadSelectHandoff:
		err = screen.ui.DrawScrollingBody(body, "Next download step", []string{screen.model.Message}, 0)
	default:
		geometry := FitScrollingList(frame.Layout.Content,
			screen.ctx.FontHeight(FontMedium)+screen.ctx.Scale(18), len(screen.model.Choices), 0)
		screen.model.VisibleRows = geometry.VisibleRows
		start := screen.model.Cursor - geometry.VisibleRows + 1
		if start < 0 {
			start = 0
		}
		for row := 0; row < geometry.VisibleRows && start+row < len(screen.model.Choices); row++ {
			index := start + row
			choice := screen.model.Choices[index]
			secondary := choice.Badge
			if secondary == "" {
				secondary = choice.Detail
			}
			if err := screen.ui.DrawListRow(geometry.Row(row), choice.Title, secondary,
				index == screen.model.Cursor); err != nil {
				return err
			}
		}
	}
	if err != nil {
		return fmt.Errorf("draw download selection state %d: %w", screen.model.State, err)
	}
	if err := frame.Finish(); err != nil {
		return fmt.Errorf("finish download selection state %d: %w", screen.model.State, err)
	}
	return nil
}

type DownloadProgressScreen struct {
	ctx   *Context
	ui    *Composer
	model *appui.DownloadProgressModel
}

func NewDownloadProgressScreen(ctx *Context, model *appui.DownloadProgressModel) (*DownloadProgressScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &DownloadProgressScreen{ctx: ctx, ui: ui, model: model}, nil
}

func (screen *DownloadProgressScreen) Close() {}

func (screen *DownloadProgressScreen) HandleInput(event InputEvent) appui.DownloadProgressIntent {
	if event.Wake {
		return appui.DownloadProgressIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *DownloadProgressScreen) Draw() error {
	footer := []FooterHint{}
	switch screen.model.State {
	case appui.DownloadProgressDone, appui.DownloadProgressError, appui.DownloadProgressCancelled:
		footer = []FooterHint{{Button: ButtonB, Label: "Back"}}
	case appui.DownloadProgressRunning:
		footer = []FooterHint{{Button: ButtonB, Label: "Cancel"}}
	case appui.DownloadProgressInhibitBlocked:
		footer = []FooterHint{
			{Button: ButtonB, Label: "Cancel"},
			{Button: ButtonA, Label: "Continue", NarrowLabel: "Go", IsConfirm: true},
		}
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{Title: screen.model.Title, Footer: footer})
	if err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	switch screen.model.State {
	case appui.DownloadProgressRunning:
		progress := float32(0)
		if screen.model.Total > 0 {
			progress = float32(screen.model.Downloaded) / float32(screen.model.Total)
		}
		detail := humanDownloadBytes(screen.model.Downloaded) + " downloaded"
		if screen.model.Total > 0 {
			detail = fmt.Sprintf("%d%%  (%s / %s)", screen.model.Downloaded*100/screen.model.Total,
				humanDownloadBytes(screen.model.Downloaded), humanDownloadBytes(screen.model.Total))
		}
		if screen.model.FileCount > 1 {
			detail = fmt.Sprintf("File %d of %d  ·  %s", screen.model.FileIndex+1, screen.model.FileCount, detail)
		}
		err = screen.ui.DrawProgressView(body, screen.model.Filename, detail, progress)
	case appui.DownloadProgressDone:
		paths := make([]string, 0, len(screen.model.SavedPaths))
		for _, path := range screen.model.SavedPaths {
			if path != "" {
				paths = append(paths, filepath.Base(path))
			}
		}
		detail := "The download was saved and added to the Leaf library."
		if len(paths) > 0 {
			detail = "Saved: " + strings.Join(paths, ", ")
		}
		err = screen.ui.DrawState(body, StateEmpty, "Download complete", detail)
	case appui.DownloadProgressInhibitBlocked:
		err = screen.ui.DrawScrollingBody(body, "Suspend protection unavailable", []string{screen.model.Detail}, 0)
	case appui.DownloadProgressCancelled:
		err = screen.ui.DrawState(body, StateOffline, "Download cancelled", screen.model.Detail)
	default:
		err = screen.ui.DrawState(body, StateError, "Download failed", screen.model.Detail)
	}
	if err != nil {
		return err
	}
	return frame.Finish()
}

func humanDownloadBytes(value int64) string {
	switch {
	case value >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(value)/1024/1024)
	case value >= 1024:
		return fmt.Sprintf("%.1f KB", float64(value)/1024)
	default:
		return fmt.Sprintf("%d B", value)
	}
}

package catui

import (
	"errors"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

func TestBoxProofGeometry960x720(t *testing.T) {
	root := NewBox(0, 0, 960, 720, 0)
	title := root.CarveTop(80)
	footer := root.CarveBottom(70)
	if title != (Box{X: 0, Y: 0, W: 960, H: 80}) {
		t.Fatalf("title carve = %+v", title)
	}
	if footer != (Box{X: 0, Y: 650, W: 960, H: 70}) {
		t.Fatalf("footer carve = %+v", footer)
	}
	root.PadTop += 16
	root.PadRight += 16
	root.PadBottom += 16
	root.PadLeft += 16

	content := root.Content()
	if content != (Rect{X: 16, Y: 96, W: 928, H: 538}) {
		t.Fatalf("content = %+v", content)
	}
	left, right := root.SplitColumns(content.W*58/100, 18)
	leftContent, rightContent := left.Content(), right.Content()
	if leftContent.W+rightContent.W+18 != content.W {
		t.Fatalf("columns do not tile content: left=%+v right=%+v content=%+v", leftContent, rightContent, content)
	}
	if leftContent.X != content.X || rightContent.X+rightContent.W != content.X+content.W {
		t.Fatalf("column edges do not match content: left=%+v right=%+v", leftContent, rightContent)
	}

	rows, visible, itemHeight := left.FitRows(60, 12, 0)
	if visible != 8 || itemHeight != 67 || rows.H != 536 {
		t.Fatalf("fit rows = rect:%+v visible:%d itemHeight:%d", rows, visible, itemHeight)
	}
}

func TestBoxHiddenFooterCostsNothing(t *testing.T) {
	root := NewBox(0, 0, 960, 720, 0)
	footer := root.CarveBottom(0)
	if footer.H != 0 || root.Content() != (Rect{0, 0, 960, 720}) {
		t.Fatalf("zero footer changed layout: footer=%+v content=%+v", footer, root.Content())
	}
}

func TestClosedContextRejectsCommands(t *testing.T) {
	ctx := &Context{initialized: true, closed: true}
	if err := ctx.DrawRect(Rect{W: 1, H: 1}, RGBA(0, 0, 0, 0)); !errors.Is(err, ErrClosed) {
		t.Fatalf("DrawRect error = %v, want %v", err, ErrClosed)
	}
	if _, err := ctx.LoadTexture("unused.png"); !errors.Is(err, ErrClosed) {
		t.Fatalf("LoadTexture error = %v, want %v", err, ErrClosed)
	}
}

func TestMenuIsNotApplicationBack(t *testing.T) {
	if got := appButton(ButtonMenu); got != appui.ButtonNone {
		t.Fatalf("Menu maps to %v, want no application action", got)
	}
	if got := appButton(ButtonB); got != appui.ButtonB {
		t.Fatalf("B maps to %v, want app back", got)
	}
}

package catui

import (
	"reflect"
	"testing"
)

func TestScreenLayout960x720WithSubHeaderAndFooter(t *testing.T) {
	layout := ComputeScreenLayout(LayoutMetrics{
		Width: 960, Height: 720,
		TitleHeight: 80, FooterHeight: 70,
		BasePadding: 16, HintsEnabled: true, HasFooterContent: true,
	}, 40)
	// Padding is applied before the title carve, so the title band starts at the
	// top pad (Y=16) and its bottom abuts the subheader (Y=96) with no orphan gap.
	if layout.Title != (Box{X: 16, Y: 16, W: 928, H: 80}) {
		t.Fatalf("title = %+v", layout.Title)
	}
	if layout.Footer != (Box{X: 16, Y: 634, W: 928, H: 70}) || !layout.FooterVisible {
		t.Fatalf("footer = %+v visible=%v", layout.Footer, layout.FooterVisible)
	}
	if layout.SubHeader != (Box{X: 16, Y: 96, W: 928, H: 40}) {
		t.Fatalf("subheader = %+v", layout.SubHeader)
	}
	if got := layout.Content.Content(); got != (Rect{X: 16, Y: 144, W: 928, H: 490}) {
		t.Fatalf("content = %+v", got)
	}
}

func TestScreenLayoutMacPreview1280x800WithoutHints(t *testing.T) {
	layout := ComputeScreenLayout(LayoutMetrics{
		Width: 1280, Height: 800,
		TitleHeight: 72, FooterHeight: 64,
		BasePadding: 20, HintsEnabled: false, HasFooterContent: true,
	}, 0)
	if layout.FooterVisible || layout.Footer != (Box{}) {
		t.Fatalf("hidden footer = %+v visible=%v", layout.Footer, layout.FooterVisible)
	}
	if got := layout.Content.Content(); got != (Rect{X: 20, Y: 92, W: 1240, H: 688}) {
		t.Fatalf("content = %+v", got)
	}
}

func TestListDetailAndFitRowsUseFinalPixels(t *testing.T) {
	body := NewBox(0, 0, 960, 600, 16)
	split := ListDetailSplit(body, 58, 18)
	left, right := split.List.Content(), split.Detail.Content()
	if left != (Rect{X: 16, Y: 16, W: 529, H: 568}) {
		t.Fatalf("left = %+v", left)
	}
	if right != (Rect{X: 563, Y: 16, W: 381, H: 568}) {
		t.Fatalf("right = %+v", right)
	}
	geometry := FitScrollingList(split.List, 60, 40, 0)
	if geometry.VisibleRows != 9 || geometry.RowHeight != 63 || geometry.Region.H != 567 {
		t.Fatalf("geometry = %+v", geometry)
	}
	if got := geometry.Row(8); got != (Rect{X: 16, Y: 520, W: 529, H: 63}) {
		t.Fatalf("last row = %+v", got)
	}
}

func TestImageGalleryAndKeyboardGeometry(t *testing.T) {
	if got := FitImage(320, 200, Rect{W: 400, H: 300}, 12); got != (Rect{X: 12, Y: 32, W: 376, H: 235}) {
		t.Fatalf("fit image = %+v", got)
	}
	gallery := LayoutGallery(Rect{W: 600, H: 400}, 3, 10, 80)
	if gallery.Main != (Rect{W: 600, H: 310}) {
		t.Fatalf("gallery main = %+v", gallery.Main)
	}
	wantThumbs := []Rect{
		{X: 0, Y: 320, W: 193, H: 80},
		{X: 203, Y: 320, W: 193, H: 80},
		{X: 406, Y: 320, W: 193, H: 80},
	}
	if !reflect.DeepEqual(gallery.Thumbnails, wantThumbs) {
		t.Fatalf("thumbnails = %+v", gallery.Thumbnails)
	}
	keyboard := LayoutKeyboard(Rect{X: 10, Y: 20, W: 300, H: 190}, [][]string{{"A", "B", "C"}, {"D", "E"}}, 10)
	if keyboard.Rows != 2 || len(keyboard.Cells) != 5 {
		t.Fatalf("keyboard = %+v", keyboard)
	}
	if keyboard.Cells[0] != (Rect{X: 10, Y: 20, W: 93, H: 90}) ||
		keyboard.Cells[4] != (Rect{X: 165, Y: 120, W: 145, H: 90}) {
		t.Fatalf("keyboard cells = %+v", keyboard.Cells)
	}
}

func TestFooterGroupingAndNarrowFallback(t *testing.T) {
	hints := []FooterHint{
		{Button: ButtonB, ButtonText: "L1/L2", NarrowButtonText: "L1/2", Label: "Previous page", NarrowLabel: "Prev"},
		{Button: ButtonX, Label: "Delete previous character", NarrowLabel: "Delete"},
		{Button: ButtonA, Label: "Confirm entered value", NarrowLabel: "Done", IsConfirm: true},
	}
	measure := func(value string) int { return len(value) * 10 }
	metrics := func(available int) FooterMeasure {
		return FooterMeasure{Available: available, Badge: 30, Margin: 5, Label: measure, ButtonText: measure}
	}
	wide := ResolveFooterGroups(hints, metrics(1000))
	if wide.Left[0].Label != "Previous page" || wide.Right[0].Label != "Confirm entered value" {
		t.Fatalf("wide labels = %+v", wide)
	}
	narrow := ResolveFooterGroups(hints, metrics(360))
	if got := []string{narrow.Left[0].Label, narrow.Left[1].Label, narrow.Right[0].Label}; !reflect.DeepEqual(got, []string{"Prev", "Delete", "Done"}) {
		t.Fatalf("narrow labels = %v", got)
	}
	if narrow.Right[0].IsConfirm != true {
		t.Fatal("confirm hint lost right-group identity")
	}
	if narrow.Left[0].ButtonText != "L1/2" {
		t.Fatalf("narrow button-text override = %q, want L1/2", narrow.Left[0].ButtonText)
	}
}

func footerLabels(groups FooterGroups) []string {
	labels := []string{}
	for _, item := range groups.Items() {
		labels = append(labels, item.Label)
	}
	return labels
}

// F6: when even narrow labels do not fit, the composer leaves out the
// lowest-ranked optional hints first and never a hint without a rank.
func TestFooterDropsRankedHintsBeforeRequiredOnes(t *testing.T) {
	hints := []FooterHint{
		{Button: ButtonB, Label: "Back"},
		{Button: ButtonL1, ButtonText: "L1/R1", Label: "Img.", DropRank: 2},
		{Button: ButtonA, Label: "Again"},
		{Button: ButtonX, Label: "Manage"},
		{Button: ButtonStart, ButtonText: "STR", Label: "Settings", NarrowLabel: "Set", DropRank: 1},
	}
	measure := func(value string) int { return len(value) * 10 }
	// Widths with a 30 px badge and 5 px margins: every hint 550, with "Set"
	// 500, without Settings 410, and without the image hint as well 290.
	cases := []struct {
		width int
		want  []string
	}{
		{550, []string{"Back", "Img.", "Again", "Manage", "Settings"}},
		{520, []string{"Back", "Img.", "Again", "Manage", "Set"}},
		{450, []string{"Back", "Img.", "Again", "Manage"}},
		{300, []string{"Back", "Again", "Manage"}},
		// Nothing left to drop: the required hints stay, and Cat's own
		// overflow hides trailing items, never the leading Back.
		{100, []string{"Back", "Again", "Manage"}},
	}
	for _, tc := range cases {
		got := footerLabels(ResolveFooterGroups(hints, FooterMeasure{
			Available: tc.width, Badge: 30, Margin: 5, Label: measure, ButtonText: measure,
		}))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("width %d: labels = %v, want %v", tc.width, got, tc.want)
		}
	}
}

// The estimate must match what cat_draw_footer draws, or Cat collapses a
// footer the composer thought would fit into a +N item.
func TestFooterMeasureMirrorsCatFooterLayout(t *testing.T) {
	measure := FooterMeasure{
		Available: 1000, Badge: 40, Margin: 10,
		Label:      func(value string) int { return len(value) * 10 },
		ButtonText: func(value string) int { return len(value) * 8 },
		ButtonName: func(button Button) string {
			if button == ButtonSelect {
				return "SELECT"
			}
			return "B"
		},
	}
	items := []FooterItem{
		{Button: ButtonB, Label: "Back"},
		{Button: ButtonStart, ButtonText: "STR", Label: "Set"},
		{Button: ButtonSelect, Label: "Apply", IsConfirm: true},
	}
	// Left pill: 10 + (40+10+40+10) + 10 + (20+24+10+30+10) + 10 = 224.
	// Right pill: 10 + (20+48+10+50+10) + 10 = 158, its badge from the name.
	if got := measure.width(items); got != 224+158 {
		t.Fatalf("footer width = %d, want %d", got, 224+158)
	}
}

func TestTagPillsWrapAndClipToBounds(t *testing.T) {
	var drawn []Rect
	height, err := drawTagPills(Rect{X: 5, Y: 7, W: 100, H: 80},
		[]string{"one", "two", "three"}, 10, 8, 6,
		func(value string) int { return len(value) * 10 },
		func(rect Rect, _ string) error {
			drawn = append(drawn, rect)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if height != 38 || len(drawn) != 3 {
		t.Fatalf("height=%d drawn=%+v", height, drawn)
	}
	for _, rect := range drawn {
		if rect.X < 5 || rect.X+rect.W > 105 || rect.Y < 7 || rect.Y+rect.H > 87 {
			t.Fatalf("pill escaped bounds: %+v", rect)
		}
	}
}

func TestContrastTextUsesLightTextOnDarkAccent(t *testing.T) {
	if got := contrastText(RGBA(40, 48, 74, 255)); got != RGBA(247, 242, 232, 255) {
		t.Fatalf("dark accent contrast = %#x", got)
	}
	if got := contrastText(RGBA(230, 220, 190, 255)); got != RGBA(24, 28, 39, 255) {
		t.Fatalf("light accent contrast = %#x", got)
	}
}

func TestWrapTextPreservesParagraphBreaks(t *testing.T) {
	lines := wrapText("one two three\n\nfour", 7, func(value string) int { return len(value) })
	want := []string{"one two", "three", "", "four"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}
}

func TestStateDetailWrapsAndEllipsizesOnlyWhatDoesNotFit(t *testing.T) {
	measure := func(value string) int { return len(value) }
	const detail = "Download stalled. Check the connection and try again."
	lines := stateDetailLines(detail, 20, 4, measure)
	want := []string{"Download stalled.", "Check the connection", "and try again."}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}
	// Too short for every line: the last one carries the rest, and the draw
	// call ellipsizes it.
	lines = stateDetailLines(detail, 20, 2, measure)
	want = []string{"Download stalled.", "Check the connection and try again."}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("capped lines = %#v, want %#v", lines, want)
	}
	if lines := stateDetailLines("", 20, 2, measure); lines != nil {
		t.Fatalf("empty detail = %#v, want no lines", lines)
	}
}

package catui

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

// Shared logical spacing. These are scaled once by NewComposer; screen code
// should not invent additional root, artwork, or modal padding values.
const (
	BasePad  = 16
	ArtPad   = 12
	ModalPad = 24
)

type LayoutMetrics struct {
	Width, Height                  int
	TitleHeight, FooterHeight      int
	BasePadding                    int
	HintsEnabled, HasFooterContent bool
}

type ScreenLayout struct {
	Screen, Title, SubHeader, Content, Footer Box
	FooterVisible                             bool
}

// ComputeScreenLayout is the single root title/content/footer carve used by
// migrated screens and by deterministic geometry tests.
func ComputeScreenLayout(metrics LayoutMetrics, subHeaderHeight int) ScreenLayout {
	// Apply the screen padding to root BEFORE carving the title, so the padding
	// sits at the top of the layout (giving the title breathing room from the
	// screen edge) instead of being orphaned as a gap between the title band and
	// the subheader. The title, subheader, and content then abut cleanly, and
	// each region owns its space from within the box model.
	pad := metrics.BasePadding
	root := NewBox(0, 0, metrics.Width, metrics.Height, 0)
	root.PadTop += pad
	root.PadRight += pad
	root.PadBottom += pad
	root.PadLeft += pad

	title := root.CarveTop(metrics.TitleHeight)
	footer := Box{}
	footerVisible := metrics.HintsEnabled && metrics.HasFooterContent
	if footerVisible {
		footer = root.CarveBottom(metrics.FooterHeight)
	}

	subHeader := Box{}
	if subHeaderHeight > 0 {
		subHeader = root.CarveTop(subHeaderHeight)
		root.CarveTop(pad / 2)
	}
	return ScreenLayout{
		Screen:        NewBox(0, 0, metrics.Width, metrics.Height, 0),
		Title:         title,
		SubHeader:     subHeader,
		Content:       root,
		Footer:        footer,
		FooterVisible: footerVisible,
	}
}

type FooterHint struct {
	Button           Button
	ButtonText       string
	NarrowButtonText string
	Label            string
	NarrowLabel      string
	IsConfirm        bool
	// DropRank marks a hint the composer may leave out when the footer does
	// not fit even with narrow labels: rank 1 goes first, then 2, and so on.
	// A hint with rank 0 is always shown.
	DropRank int
}

type FooterGroups struct {
	Left, Right []FooterItem
}

func (g FooterGroups) Items() []FooterItem {
	items := make([]FooterItem, 0, len(g.Left)+len(g.Right))
	items = append(items, g.Left...)
	items = append(items, g.Right...)
	return items
}

// FooterMeasure describes cat_draw_footer in final pixels: the width the
// footer may fill, the round button badge, the margin around badges, labels,
// and groups, and how to measure label and badge text.
type FooterMeasure struct {
	Available, Badge, Margin int
	Label, ButtonText        func(string) int
	// ButtonName is the badge text Cat draws for a hint without ButtonText.
	// Without it, such a hint is measured as a round badge.
	ButtonName func(Button) string
}

// width mirrors cat_draw_footer. Each group is an outer pill with a margin at
// both ends and between items. An item is its badge, a margin, its label, and
// a margin. A single-character badge is round; a longer one is half a round
// badge plus its text in Cat's tiny font.
func (measure FooterMeasure) width(items []FooterItem) int {
	total := 0
	for _, confirm := range []bool{false, true} {
		count := 0
		for _, item := range items {
			if item.IsConfirm != confirm {
				continue
			}
			text := item.ButtonText
			if text == "" && measure.ButtonName != nil {
				text = measure.ButtonName(item.Button)
			}
			badge := measure.Badge
			if text != "" && utf8.RuneCountInString(text) != 1 {
				badge = measure.Badge/2 + measure.ButtonText(text)
			}
			total += badge + measure.Margin + measure.Label(item.Label) + measure.Margin
			if count > 0 {
				total += measure.Margin
			}
			count++
		}
		if count > 0 {
			total += measure.Margin * 2
		}
	}
	return total
}

// ResolveFooterGroups preserves Catastrophe's left/action and right/confirm
// grouping, switching all labels to their narrow variants when the footer
// would not fit. When even narrow labels do not fit, it leaves out hints by
// DropRank, lowest first, and tries wide labels again, so Cat never has to
// collapse the footer into a synthetic +N item. Catastrophe remains the
// final footer renderer.
func ResolveFooterGroups(hints []FooterHint, measure FooterMeasure) FooterGroups {
	shown := make([]bool, len(hints))
	for i := range hints {
		shown[i] = true
	}
	resolve := func(narrow bool) []FooterItem {
		items := make([]FooterItem, 0, len(hints))
		for i, hint := range hints {
			if !shown[i] {
				continue
			}
			item := FooterItem{Button: hint.Button, ButtonText: hint.ButtonText, Label: hint.Label, IsConfirm: hint.IsConfirm}
			if narrow && hint.NarrowLabel != "" {
				item.Label = hint.NarrowLabel
			}
			if narrow && hint.NarrowButtonText != "" {
				item.ButtonText = hint.NarrowButtonText
			}
			items = append(items, item)
		}
		return items
	}
	dropNext := func() bool {
		drop := -1
		for i := range hints {
			if shown[i] && hints[i].DropRank > 0 && (drop < 0 || hints[i].DropRank < hints[drop].DropRank) {
				drop = i
			}
		}
		if drop >= 0 {
			shown[drop] = false
		}
		return drop >= 0
	}
	var items []FooterItem
	for {
		if items = resolve(false); measure.width(items) <= measure.Available {
			break
		}
		if items = resolve(true); measure.width(items) <= measure.Available || !dropNext() {
			break
		}
	}

	groups := FooterGroups{}
	for _, item := range items {
		if item.IsConfirm {
			groups.Right = append(groups.Right, item)
		} else {
			groups.Left = append(groups.Left, item)
		}
	}
	return groups
}

type Composer struct {
	ctx                                   *Context
	BasePadding, ArtPadding, ModalPadding int
}

func NewComposer(ctx *Context) (*Composer, error) {
	if err := ctx.ensureOpen(); err != nil {
		return nil, err
	}
	return &Composer{
		ctx:          ctx,
		BasePadding:  ctx.Scale(BasePad),
		ArtPadding:   ctx.Scale(ArtPad),
		ModalPadding: ctx.Scale(ModalPad),
	}, nil
}

type ScreenSpec struct {
	Title           string
	SubHeaderHeight int
	Footer          []FooterHint
}

type ScreenFrame struct {
	Layout ScreenLayout
	footer []FooterItem
	ui     *Composer
}

func (ui *Composer) BeginScreen(spec ScreenSpec) (*ScreenFrame, error) {
	// A screen frame owns the full renderer. Never let a content clip inherited
	// from a previous draw suppress background or inherited title/status chrome.
	if err := ui.ctx.ResetClip(); err != nil {
		return nil, err
	}
	if err := ui.ctx.Clear(); err != nil {
		return nil, err
	}
	width, height, err := ui.ctx.ScreenSize()
	if err != nil {
		return nil, err
	}
	available, badge, margin := ui.ctx.FooterMetrics()
	groups := ResolveFooterGroups(spec.Footer, FooterMeasure{
		Available: available, Badge: badge, Margin: margin,
		Label:      func(text string) int { return ui.ctx.MeasureText(FontSmall, text) },
		ButtonText: func(text string) int { return ui.ctx.MeasureText(FontTiny, text) },
		ButtonName: ui.ctx.ButtonName,
	})
	footer := groups.Items()
	layout := ComputeScreenLayout(LayoutMetrics{
		Width:            width,
		Height:           height,
		TitleHeight:      ui.ctx.TitleHeight(),
		FooterHeight:     ui.ctx.FooterHeight(),
		BasePadding:      ui.BasePadding,
		HintsEnabled:     ui.ctx.HintsEnabled(),
		HasFooterContent: len(footer) > 0,
	}, spec.SubHeaderHeight)
	if err := ui.ctx.DrawTitleIn(layout.Title.Content(), spec.Title); err != nil {
		return nil, err
	}
	return &ScreenFrame{Layout: layout, footer: footer, ui: ui}, nil
}

func (frame *ScreenFrame) Finish() error {
	if frame == nil || frame.ui == nil {
		return ErrClosed
	}
	// Footer chrome is outside content clips by definition.
	if err := frame.ui.ctx.ResetClip(); err != nil {
		return err
	}
	if frame.Layout.FooterVisible {
		return frame.ui.ctx.DrawFooter(frame.footer)
	}
	return nil
}

func (ui *Composer) DrawSubHeader(box Box, text string) error {
	rect := box.Content()
	y := rect.Y + (rect.H-ui.ctx.FontHeight(FontSmall))/2
	_, err := ui.ctx.DrawText(FontSmall, text, rect.X, y,
		ui.ctx.ThemeColor(RoleHint), rect.W, true)
	return err
}

type ListDetailLayout struct{ List, Detail Box }

// previewCardColor matches the subtle rounded panel the native launcher fills
// behind its games/apps/recents preview pane (#ffffff10).
var previewCardColor = RGBA(0xff, 0xff, 0xff, 0x10)

func ListDetailSplit(body Box, leftPercent, gutter int) ListDetailLayout {
	if leftPercent < 0 {
		leftPercent = 0
	}
	if leftPercent > 100 {
		leftPercent = 100
	}
	content := body.Content()
	left, right := body.SplitColumns(content.W*leftPercent/100, gutter)
	return ListDetailLayout{List: left, Detail: right}
}

// DrawPreviewCard fills the games-style rounded panel behind a preview pane and
// returns the padded inner rect to draw the preview content into, so list/detail
// screens match the games/apps/recents gutter.
func (ui *Composer) DrawPreviewCard(pane Rect) (Rect, error) {
	if err := ui.ctx.DrawRoundedRect(pane, ui.ctx.Scale(8), previewCardColor); err != nil {
		return Rect{}, err
	}
	return insetRect(pane, ui.ArtPadding, ui.ArtPadding), nil
}

func FullWidthBody(body Box) Rect { return body.Content() }

type ListGeometry struct {
	Region                 Rect
	VisibleRows, RowHeight int
}

func FitScrollingList(box Box, baseRowHeight, itemCount, cachedVisibleRows int) ListGeometry {
	region, visible, rowHeight := box.FitRows(baseRowHeight, itemCount, cachedVisibleRows)
	return ListGeometry{Region: region, VisibleRows: visible, RowHeight: rowHeight}
}

func (g ListGeometry) Row(index int) Rect {
	if index < 0 || index >= g.VisibleRows {
		return Rect{}
	}
	return Rect{X: g.Region.X, Y: g.Region.Y + index*g.RowHeight, W: g.Region.W, H: g.RowHeight}
}

func (ui *Composer) DrawListRow(rect Rect, primary, secondary string, selected bool) error {
	if rect.W <= 0 || rect.H <= 0 {
		return nil
	}
	if selected {
		pill := insetRect(rect, 0, ui.ctx.Scale(3))
		// Reserve the same right-edge margin the native launcher does
		// (iw - CAT_S(4)) so the pill clears the scrollbar/gutter.
		pill.W -= ui.ctx.Scale(4)
		if err := ui.ctx.DrawPill(pill, ui.ctx.ThemeColor(RoleHighlight)); err != nil {
			return err
		}
	}
	return ui.withClip(rect, func() error {
		primaryRole, secondaryRole := listRowRoles(selected)
		pad := ui.ctx.Scale(12)
		x := rect.X + pad
		primaryY := rect.Y + (rect.H-ui.ctx.FontHeight(FontMedium))/2
		maxWidth := rect.W - pad*2
		if secondary != "" {
			secondaryWidth := minInt(ui.ctx.MeasureText(FontTiny, secondary), maxInt(0, rect.W/3))
			maxWidth -= secondaryWidth + pad
			if secondaryWidth > 0 {
				secondaryY := rect.Y + (rect.H-ui.ctx.FontHeight(FontTiny))/2
				if _, err := ui.ctx.DrawText(FontTiny, secondary,
					rect.X+rect.W-pad-secondaryWidth, secondaryY,
					ui.ctx.ThemeColor(secondaryRole), secondaryWidth, true); err != nil {
					return err
				}
			}
		}
		if primary == "" || maxWidth <= 0 {
			return nil
		}
		_, err := ui.DrawEllipsizedText(FontMedium, primary, x, primaryY, ui.ctx.ThemeColor(primaryRole), maxWidth)
		return err
	})
}

// listRowRoles colors a list row's title and its badge or price. On the
// highlighted row both use the highlighted text color: the hint color is
// barely readable on the highlight.
func listRowRoles(selected bool) (primary, secondary ThemeRole) {
	if selected {
		return RoleHighlightedText, RoleHighlightedText
	}
	return RoleText, RoleHint
}

// DrawEllipsizedText draws text with the fallback fonts and ends it in "..."
// when it is wider than maxWidth.
func (ui *Composer) DrawEllipsizedText(tier FontTier, text string, x, y int, color Color, maxWidth int) (int, error) {
	text = ellipsizeText(text, maxWidth, func(value string) int {
		return ui.ctx.MeasureFallbackText(tier, value)
	})
	return ui.ctx.DrawFallbackText(tier, text, x, y, color, maxWidth)
}

// ellipsis matches what Catastrophe's cat_draw_text_ellipsized appends.
const ellipsis = "..."

// ellipsizeText shortens text to the longest start that fits maxWidth with
// "..." after it. It cuts between characters, and drops spaces and zero-width
// joiners before the dots. When not even one character fits with the dots,
// it returns text unchanged for the draw call's clip.
func ellipsizeText(text string, maxWidth int, measure func(string) int) string {
	if text == "" || maxWidth <= 0 || measure(text) <= maxWidth {
		return text
	}
	cuts := make([]int, 0, len(text))
	for offset := range text {
		cuts = append(cuts, offset)
	}
	// cuts[0] is the empty start; find the last cut whose start still fits.
	low, high := 0, len(cuts)-1
	for low < high {
		middle := (low + high + 1) / 2
		if measure(text[:cuts[middle]]+ellipsis) <= maxWidth {
			low = middle
		} else {
			high = middle - 1
		}
	}
	start := strings.TrimRightFunc(text[:cuts[low]], func(r rune) bool {
		return unicode.IsSpace(r) || r == '‍'
	})
	if start == "" {
		return text
	}
	return start + ellipsis
}

func (ui *Composer) DrawValueRow(rect Rect, label, value string, selected, cycler bool) error {
	if rect.W <= 0 || rect.H <= 0 {
		return nil
	}
	if selected {
		pill := insetRect(rect, 0, ui.ctx.Scale(3))
		if err := ui.ctx.DrawPill(pill, ui.ctx.ThemeColor(RoleHighlight)); err != nil {
			return err
		}
	}
	return ui.withClip(rect, func() error {
		pad := ui.ctx.Scale(12)
		arrow := ui.ctx.Scale(10)
		valueWidth := minInt(ui.ctx.MeasureText(FontSmall, value), maxInt(0, rect.W/2))
		valueX := rect.X + rect.W - pad - valueWidth
		if cycler {
			valueX -= arrow*2 + ui.ctx.Scale(12)
		}
		color := ui.ctx.ThemeColor(RoleHint)
		if selected {
			color = ui.ctx.ThemeColor(RoleHighlightedText)
		}
		y := rect.Y + (rect.H-ui.ctx.FontHeight(FontSmall))/2
		if value != "" && valueWidth > 0 {
			if _, err := ui.ctx.DrawText(FontSmall, value, valueX, y, color, valueWidth, true); err != nil {
				return err
			}
		}
		labelWidth := maxInt(0, valueX-rect.X-pad*2)
		if label != "" && labelWidth > 0 {
			labelY := rect.Y + (rect.H-ui.ctx.FontHeight(FontMedium))/2
			if _, err := ui.ctx.DrawFallbackText(FontMedium, label, rect.X+pad, labelY, color, labelWidth); err != nil {
				return err
			}
		}
		if cycler {
			triY := rect.Y + (rect.H-arrow)/2
			if err := ui.ctx.DrawTriangle(Rect{valueX - arrow - ui.ctx.Scale(5), triY, arrow, arrow}, DirectionLeft, color); err != nil {
				return err
			}
			return ui.ctx.DrawTriangle(Rect{rect.X + rect.W - pad - arrow, triY, arrow, arrow}, DirectionRight, color)
		}
		return nil
	})
}

// DrawScrollingBody draws title in the large font and paragraphs below it in
// rect, with a blank line between paragraphs. Only the body lines scroll:
// scroll, when not nil, says which line to start at and gets the bounds of
// what fits; nil draws from the first line. Body lines that do not fit get
// the launcher scrollbar at rect's right edge, and wrap narrower to leave
// room for it.
func (ui *Composer) DrawScrollingBody(rect Rect, title string, paragraphs []string, scroll *appui.BodyScroll) error {
	layout := layoutBody(rect, title, paragraphs, ui.bodyMetrics())
	offset := 0
	if scroll != nil {
		scroll.SetScrollBounds(layout.maxOffset())
		offset = scroll.ScrollLine
	}
	offset = minInt(maxInt(offset, 0), layout.maxOffset())
	return ui.withClip(rect, func() error {
		x, y := rect.X, rect.Y
		for _, line := range layout.titleLines {
			if _, err := ui.ctx.DrawFallbackText(FontLarge, line, x, y,
				ui.ctx.ThemeColor(RoleEmphasis), rect.W); err != nil {
				return err
			}
			y += ui.ctx.FontHeight(FontLarge)
		}
		y = rect.Y + layout.top
		lineHeight := ui.bodyLineHeight()
		for i := offset; i < len(layout.lines) && y+lineHeight <= rect.Y+rect.H; i++ {
			if layout.lines[i] != "" {
				if _, err := ui.ctx.DrawFallbackText(FontSmall, layout.lines[i], x, y,
					ui.ctx.ThemeColor(RoleText), layout.width); err != nil {
					return err
				}
			}
			y += lineHeight
		}
		track := rect.H - layout.top
		if !layout.overflows() || track <= 0 {
			return nil
		}
		// The launcher list's scrollbar, beside the lines that scroll.
		return ui.ctx.DrawScrollbar(rect.X+rect.W-ui.ctx.Scale(scrollbarWidth), rect.Y+layout.top,
			track, layout.rows, len(layout.lines), offset)
	})
}

// scrollbarWidth is Cat's scrollbar width, and scrollbarGutter the room
// cat_draw_scroll_view leaves for it beside scrolling content.
const (
	scrollbarWidth  = 4
	scrollbarGutter = 12
)

func (ui *Composer) bodyLineHeight() int { return ui.ctx.FontHeight(FontSmall) + ui.ctx.Scale(5) }

func (ui *Composer) bodyMetrics() bodyMetrics {
	return bodyMetrics{
		titleHeight: ui.ctx.FontHeight(FontLarge),
		titleGap:    ui.BasePadding / 2,
		lineHeight:  ui.bodyLineHeight(),
		gutter:      ui.ctx.Scale(scrollbarGutter),
		measureTitle: func(value string) int {
			return ui.ctx.MeasureFallbackText(FontLarge, value)
		},
		measureLine: func(value string) int { return ui.ctx.MeasureText(FontSmall, value) },
	}
}

// bodyMetrics are the sizes DrawScrollingBody lays text out with: the title
// line height and the gap under the title, the body line pitch, the room a
// scrollbar takes, and how to measure title and body text.
type bodyMetrics struct {
	titleHeight, titleGap, lineHeight, gutter int
	measureTitle, measureLine                 func(string) int
}

// bodyLayout is a scrolling body laid out in a rect: the title lines, the
// body lines with a blank line between paragraphs, the width the body
// lines wrap to, how far below the rect's top they start, and how many of
// them fit.
type bodyLayout struct {
	titleLines, lines []string
	width, top, rows  int
}

func (layout bodyLayout) overflows() bool { return len(layout.lines) > layout.rows }

// maxOffset is the first line that puts the last line at the bottom.
func (layout bodyLayout) maxOffset() int { return maxInt(0, len(layout.lines)-layout.rows) }

func layoutBody(rect Rect, title string, paragraphs []string, metrics bodyMetrics) bodyLayout {
	layout := bodyLayout{width: rect.W}
	if title != "" {
		// A title wider than a narrow column wraps instead of being cut off.
		layout.titleLines = wrapText(title, rect.W, metrics.measureTitle)
		layout.top = len(layout.titleLines)*metrics.titleHeight + metrics.titleGap
	}
	layout.rows = 1
	if metrics.lineHeight > 0 {
		layout.rows = maxInt(1, (rect.H-layout.top)/metrics.lineHeight)
	}
	layout.lines = bodyLines(paragraphs, layout.width, metrics.measureLine)
	// Narrower lines only add lines, so a body that overflows at the full
	// width still overflows beside the scrollbar.
	if layout.overflows() && rect.W > metrics.gutter {
		layout.width = rect.W - metrics.gutter
		layout.lines = bodyLines(paragraphs, layout.width, metrics.measureLine)
	}
	return layout
}

func bodyLines(paragraphs []string, width int, measure func(string) int) []string {
	lines := make([]string, 0, len(paragraphs)*2)
	for index, paragraph := range paragraphs {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, wrapText(paragraph, width, measure)...)
	}
	return lines
}

// QRCaptionHeight is the room DrawQRCaption takes for caption at width.
func (ui *Composer) QRCaptionHeight(caption string, width int) int {
	return ui.ctx.Scale(5) + len(ui.qrCaptionLines(caption, width))*ui.ctx.FontHeight(FontTiny)
}

// DrawQRCaption draws caption in the hint color under the QR code drawn at
// qr, from x and at most width wide. Detail and About caption their QR
// codes with it. A caption too wide for one line takes two, and the
// second ends in "..." if it still does not fit. It returns the y below
// the caption.
func (ui *Composer) DrawQRCaption(qr Rect, x, width int, caption string) (int, error) {
	y := qr.Y + qr.H + ui.ctx.Scale(5)
	for _, line := range ui.qrCaptionLines(caption, width) {
		if _, err := ui.ctx.DrawText(FontTiny, line, x, y, ui.ctx.ThemeColor(RoleHint), width, true); err != nil {
			return y, err
		}
		y += ui.ctx.FontHeight(FontTiny)
	}
	return y, nil
}

func (ui *Composer) qrCaptionLines(caption string, width int) []string {
	return fitLines(caption, width, 2, func(value string) int { return ui.ctx.MeasureText(FontTiny, value) })
}

func CenteredModalRect(bounds Rect, widthPercent, heightPercent, margin int) Rect {
	if widthPercent <= 0 || widthPercent > 100 {
		widthPercent = 72
	}
	if heightPercent <= 0 || heightPercent > 100 {
		heightPercent = 56
	}
	w, h := bounds.W*widthPercent/100, bounds.H*heightPercent/100
	w = maxInt(0, w-margin*2)
	h = maxInt(0, h-margin*2)
	return Rect{X: bounds.X + (bounds.W-w)/2, Y: bounds.Y + (bounds.H-h)/2, W: w, H: h}
}

func (ui *Composer) DrawModal(bounds Rect, title, body string) error {
	widthPercent, heightPercent := 72, 58
	if bounds.W < ui.ctx.Scale(600) {
		widthPercent, heightPercent = 90, 72
	}
	modal := CenteredModalRect(bounds, widthPercent, heightPercent, 0)
	if err := ui.ctx.DrawRect(modal, ui.ctx.ThemeColor(RoleAccent)); err != nil {
		return err
	}
	if err := ui.ctx.DrawPill(Rect{X: modal.X, Y: modal.Y, W: modal.W, H: ui.ctx.Scale(5)},
		ui.ctx.ThemeColor(RoleHighlight)); err != nil {
		return err
	}
	inner := insetRect(modal, ui.ModalPadding, ui.ModalPadding)
	if _, err := ui.ctx.DrawFallbackText(FontLarge, title, inner.X, inner.Y,
		ui.ctx.ThemeColor(RoleEmphasis), inner.W); err != nil {
		return err
	}
	inner.Y += ui.ctx.FontHeight(FontLarge) + ui.BasePadding
	inner.H -= ui.ctx.FontHeight(FontLarge) + ui.BasePadding
	return ui.DrawScrollingBody(inner, "", []string{body}, nil)
}

func (ui *Composer) DrawWarningCover(bounds Rect, title, body string) error {
	if err := ui.ctx.DrawRect(bounds, ui.ctx.ThemeColor(RoleHighlight)); err != nil {
		return err
	}
	inner := insetRect(bounds, ui.ModalPadding, ui.ModalPadding)
	color := ui.ctx.ThemeColor(RoleHighlightedText)
	if _, err := ui.ctx.DrawFallbackText(FontLarge, title, inner.X, inner.Y, color, inner.W); err != nil {
		return err
	}
	y := inner.Y + ui.ctx.FontHeight(FontLarge) + ui.BasePadding/2
	for _, line := range wrapText(body, inner.W, func(value string) int {
		return ui.ctx.MeasureText(FontSmall, value)
	}) {
		if _, err := ui.ctx.DrawText(FontSmall, line, inner.X, y, color, inner.W, false); err != nil {
			return err
		}
		y += ui.ctx.FontHeight(FontSmall) + ui.ctx.Scale(4)
	}
	return nil
}

func (ui *Composer) DrawProgressView(bounds Rect, title, detail string, progress float32) error {
	inner := insetRect(bounds, ui.ModalPadding, ui.ModalPadding)
	titleHeight := ui.ctx.FontHeight(FontLarge)
	// The title says what is happening to which file ("Inspecting
	// <upload>.zip"). A long name wraps instead of being cut off; a
	// one-line title keeps its place.
	titles := progressTitleLines(title, inner.W, func(value string) int {
		return ui.ctx.MeasureFallbackText(FontLarge, value)
	})
	extra := maxInt(0, len(titles)-1) * titleHeight
	y := inner.Y + maxInt(0, (inner.H-ui.ctx.Scale(84)-extra)/2)
	for _, line := range titles {
		if _, err := ui.ctx.DrawFallbackText(FontLarge, line, inner.X, y,
			ui.ctx.ThemeColor(RoleEmphasis), inner.W); err != nil {
			return err
		}
		y += titleHeight
	}
	if len(titles) == 0 {
		y += titleHeight
	}
	y += ui.BasePadding / 2
	if _, err := ui.ctx.DrawText(FontSmall, detail, inner.X, y,
		ui.ctx.ThemeColor(RoleHint), inner.W, true); err != nil {
		return err
	}
	y += ui.ctx.FontHeight(FontSmall) + ui.BasePadding
	return ui.ctx.DrawProgress(Rect{X: inner.X, Y: y, W: inner.W, H: ui.ctx.Scale(9)},
		progress, ui.ctx.ThemeColor(RoleHighlight), ui.ctx.ThemeColor(RoleDisabled))
}

func (ui *Composer) DrawTextField(bounds Rect, label, value string, active bool) error {
	color := ui.ctx.ThemeColor(RoleAccent)
	if active {
		color = ui.ctx.ThemeColor(RoleHighlight)
	}
	if err := ui.ctx.DrawPill(bounds, color); err != nil {
		return err
	}
	inner := insetRect(bounds, ui.ctx.Scale(12), ui.ctx.Scale(5))
	textColor := contrastText(color)
	text := value
	if text == "" {
		text = label
	}
	y := inner.Y + (inner.H-ui.ctx.FontHeight(FontSmall))/2
	_, err := ui.ctx.DrawFallbackText(FontSmall, text, inner.X, y, textColor, inner.W)
	return err
}

type KeyboardLayout struct {
	Cells []Rect
	Rows  int
}

func LayoutKeyboard(bounds Rect, keys [][]string, gap int) KeyboardLayout {
	layout := KeyboardLayout{Rows: len(keys)}
	if len(keys) == 0 {
		return layout
	}
	rowHeight := (bounds.H - gap*(len(keys)-1)) / len(keys)
	y := bounds.Y
	for _, row := range keys {
		if len(row) == 0 {
			y += rowHeight + gap
			continue
		}
		cellWidth := (bounds.W - gap*(len(row)-1)) / len(row)
		x := bounds.X
		for range row {
			layout.Cells = append(layout.Cells, Rect{X: x, Y: y, W: cellWidth, H: rowHeight})
			x += cellWidth + gap
		}
		y += rowHeight + gap
	}
	return layout
}

func (ui *Composer) DrawKeyboard(bounds Rect, keys [][]string, selected int) error {
	layout := LayoutKeyboard(bounds, keys, ui.ctx.Scale(5))
	index := 0
	for _, row := range keys {
		for _, key := range row {
			cell := layout.Cells[index]
			active := index == selected
			if active {
				if err := ui.ctx.DrawPill(cell, ui.ctx.ThemeColor(RoleHighlight)); err != nil {
					return err
				}
			}
			color := ui.ctx.ThemeColor(RoleText)
			if active {
				color = ui.ctx.ThemeColor(RoleHighlightedText)
			}
			width := ui.ctx.MeasureText(FontTiny, key)
			if width > cell.W {
				width = cell.W
			}
			x := cell.X + (cell.W-width)/2
			y := cell.Y + (cell.H-ui.ctx.FontHeight(FontTiny))/2
			if _, err := ui.ctx.DrawText(FontTiny, key, x, y, color, cell.W, true); err != nil {
				return err
			}
			index++
		}
	}
	return nil
}

func (ui *Composer) DrawTagPills(bounds Rect, tags []string) (int, error) {
	background := ui.ctx.ThemeColor(RoleAccent)
	return drawTagPills(bounds, tags, ui.ctx.FontHeight(FontTiny), ui.ctx.Scale(8), ui.ctx.Scale(6),
		func(value string) int { return ui.ctx.MeasureFallbackText(FontTiny, value) },
		func(rect Rect, value string) error {
			if err := ui.ctx.DrawPill(rect, background); err != nil {
				return err
			}
			y := rect.Y + (rect.H-ui.ctx.FontHeight(FontTiny))/2
			_, err := ui.ctx.DrawFallbackText(FontTiny, value, rect.X+ui.ctx.Scale(8), y,
				contrastText(background), rect.W-ui.ctx.Scale(16))
			return err
		})
}

func contrastText(background Color) Color {
	r := int(uint32(background) & 0xff)
	g := int((uint32(background) >> 8) & 0xff)
	b := int((uint32(background) >> 16) & 0xff)
	// Integer approximation of perceived luminance. A slightly conservative
	// threshold keeps small Cat font tiers readable on saturated colours.
	if (r*299+g*587+b*114)/1000 >= 145 {
		return RGBA(24, 28, 39, 255)
	}
	return RGBA(247, 242, 232, 255)
}

func drawTagPills(bounds Rect, tags []string, fontHeight, horizontalPad, gap int,
	measure func(string) int, draw func(Rect, string) error) (int, error) {
	if len(tags) == 0 || bounds.W <= 0 {
		return 0, nil
	}
	pillHeight := fontHeight + gap
	x, y := bounds.X, bounds.Y
	for _, tag := range tags {
		width := minInt(bounds.W, measure(tag)+horizontalPad*2)
		if x > bounds.X && x+width > bounds.X+bounds.W {
			x = bounds.X
			y += pillHeight + gap
		}
		if y+pillHeight > bounds.Y+bounds.H {
			break
		}
		if err := draw(Rect{X: x, Y: y, W: width, H: pillHeight}, tag); err != nil {
			return y - bounds.Y, err
		}
		x += width + gap
	}
	return minInt(bounds.H, y-bounds.Y+pillHeight), nil
}

func FitImage(sourceWidth, sourceHeight int, viewport Rect, artPadding int) Rect {
	inner := insetRect(viewport, artPadding, artPadding)
	if sourceWidth <= 0 || sourceHeight <= 0 || inner.W <= 0 || inner.H <= 0 {
		return Rect{X: inner.X, Y: inner.Y}
	}
	w, h := inner.W, sourceHeight*inner.W/sourceWidth
	if h > inner.H {
		h = inner.H
		w = sourceWidth * inner.H / sourceHeight
	}
	return Rect{X: inner.X + (inner.W-w)/2, Y: inner.Y + (inner.H-h)/2, W: w, H: h}
}

func (ui *Composer) DrawImageFit(texture *Texture, viewport Rect) error {
	width, height, err := texture.Size()
	if err != nil {
		return err
	}
	return ui.withClip(viewport, func() error {
		return texture.Draw(FitImage(width, height, viewport, ui.ArtPadding))
	})
}

type GalleryLayout struct {
	Main       Rect
	Thumbnails []Rect
}

func LayoutGallery(bounds Rect, count, gap, thumbnailHeight int) GalleryLayout {
	if count < 0 {
		count = 0
	}
	thumbBand := 0
	if count > 1 {
		thumbBand = thumbnailHeight + gap
	}
	main := Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: maxInt(0, bounds.H-thumbBand)}
	layout := GalleryLayout{Main: main}
	if count <= 1 {
		return layout
	}
	if gap*(count-1) > bounds.W-count {
		gap = maxInt(0, (bounds.W-count)/(count-1))
	}
	width := maxInt(1, (bounds.W-gap*(count-1))/count)
	y := bounds.Y + main.H + gap
	x := bounds.X
	for i := 0; i < count; i++ {
		layout.Thumbnails = append(layout.Thumbnails, Rect{X: x, Y: y, W: width, H: thumbnailHeight})
		x += width + gap
	}
	return layout
}

func (ui *Composer) DrawGallery(bounds Rect, textures []*Texture, selected int) error {
	if len(textures) == 0 {
		return ui.DrawState(bounds, StateEmpty, "No screenshots", "This game has no gallery images.")
	}
	if selected < 0 || selected >= len(textures) {
		selected = 0
	}
	layout := LayoutGallery(bounds, len(textures), ui.ctx.Scale(7), ui.ctx.Scale(74))
	if err := ui.DrawImageFit(textures[selected], layout.Main); err != nil {
		return err
	}
	if len(textures) == 1 {
		return nil
	}
	for index, texture := range textures {
		if index == selected {
			if err := ui.ctx.DrawPill(layout.Thumbnails[index], ui.ctx.ThemeColor(RoleHighlight)); err != nil {
				return err
			}
		}
		thumb := insetRect(layout.Thumbnails[index], ui.ctx.Scale(3), ui.ctx.Scale(3))
		if err := ui.DrawImageFit(texture, thumb); err != nil {
			return err
		}
	}
	return nil
}

type StateKind int

const (
	StateEmpty StateKind = iota
	StateLoading
	StateOffline
	StateError
)

func (kind StateKind) label() string {
	switch kind {
	case StateLoading:
		return "Loading"
	case StateOffline:
		return "Offline"
	case StateError:
		return "Error"
	default:
		return "Empty"
	}
}

func (ui *Composer) DrawState(bounds Rect, kind StateKind, title, detail string) error {
	if title == "" {
		title = kind.label()
	}
	inner := insetRect(bounds, ui.ModalPadding, ui.ModalPadding)
	titleHeight := ui.ctx.FontHeight(FontLarge)
	detailHeight := ui.ctx.FontHeight(FontSmall)
	lineHeight := detailHeight + ui.ctx.Scale(4)
	gap := ui.BasePadding / 2
	// The detail wraps instead of being cut off at the first line, so an error
	// stays readable with a larger font. In short bounds, the last line that
	// fits is ellipsized.
	maxLines := 1 + maxInt(0, inner.H-titleHeight-gap-detailHeight)/lineHeight
	lines := fitLines(detail, inner.W, maxLines, func(value string) int {
		return ui.ctx.MeasureText(FontSmall, value)
	})
	total := titleHeight + gap + detailHeight + maxInt(0, len(lines)-1)*lineHeight
	y := inner.Y + maxInt(0, (inner.H-total)/2)
	color := ui.ctx.ThemeColor(RoleEmphasis)
	if kind == StateError {
		color = ui.ctx.ThemeColor(RoleHighlight)
	}
	width := ui.ctx.MeasureFallbackText(FontLarge, title)
	if width > inner.W {
		width = inner.W
	}
	if _, err := ui.ctx.DrawFallbackText(FontLarge, title, inner.X+(inner.W-width)/2,
		y, color, width); err != nil {
		return err
	}
	y += titleHeight + gap
	for _, line := range lines {
		width := minInt(inner.W, ui.ctx.MeasureText(FontSmall, line))
		if _, err := ui.ctx.DrawText(FontSmall, line, inner.X+(inner.W-width)/2,
			y, ui.ctx.ThemeColor(RoleHint), width, true); err != nil {
			return err
		}
		y += lineHeight
	}
	return nil
}

// fitLines wraps a message to width and keeps at most maxLines lines. When
// the message needs more, the last kept line carries the rest so it can be
// ellipsized instead of dropping words silently: DrawText does that itself,
// fallback-font text goes through ellipsizeLine.
func fitLines(text string, width, maxLines int, measure func(string) int) []string {
	if text == "" {
		return nil
	}
	lines := wrapText(text, width, measure)
	maxLines = maxInt(1, maxLines)
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], strings.Join(lines[maxLines-1:], " "))
	}
	return lines
}

// progressTitleLines fits a progress title on at most two lines.
func progressTitleLines(title string, width int, measure func(string) int) []string {
	lines := fitLines(title, width, 2, measure)
	if len(lines) > 0 {
		lines[len(lines)-1] = ellipsizeLine(lines[len(lines)-1], width, measure)
	}
	return lines
}

// ellipsizeLine shortens line to width with a trailing "...", the way Cat
// ellipsizes DrawText, for fallback-font text that Cat only clips. A width
// too narrow for "..." leaves the line for the draw call to clip.
func ellipsizeLine(line string, width int, measure func(string) int) string {
	if measure(line) <= width {
		return line
	}
	const ellipsis = "..."
	target := width - measure(ellipsis)
	if target <= 0 {
		return line
	}
	runes := []rune(line)
	best, lo, hi := 0, 0, len(runes)
	for lo <= hi {
		mid := (lo + hi) / 2
		if measure(string(runes[:mid])) <= target {
			best, lo = mid, mid+1
		} else {
			hi = mid - 1
		}
	}
	return strings.TrimRight(string(runes[:best]), " ") + ellipsis
}

func (ui *Composer) withClip(rect Rect, draw func() error) error {
	if err := ui.ctx.SetClip(rect); err != nil {
		return err
	}
	drawErr := draw()
	resetErr := ui.ctx.ResetClip()
	return errors.Join(drawErr, resetErr)
}

func insetRect(rect Rect, horizontal, vertical int) Rect {
	return Rect{
		X: rect.X + horizontal,
		Y: rect.Y + vertical,
		W: maxInt(0, rect.W-horizontal*2),
		H: maxInt(0, rect.H-vertical*2),
	}
}

func wrapText(text string, maxWidth int, measure func(string) int) []string {
	if maxWidth <= 0 {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			candidate := line + " " + word
			if measure(candidate) <= maxWidth {
				line = candidate
				continue
			}
			lines = append(lines, line)
			line = word
		}
		lines = append(lines, line)
	}
	return lines
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

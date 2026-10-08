package appui

import (
	"strings"

	"golang.org/x/net/html"
)

type DetailState uint8

const (
	DetailLoading DetailState = iota
	DetailReady
	DetailWarning
	DetailError
)

type DetailGame struct {
	Title, Author, URL, Platform    string
	Price                           float64
	IsFree, Downloaded, CanDownload bool
	// NeedsSignIn marks a paid game that can download once the user signs
	// in with itch.io; A then opens sign-in instead.
	NeedsSignIn bool
	// Owned is set when the game is in the signed-in account's owned set.
	// A paid game that is neither downloadable nor waiting for a sign-in is
	// one that account does not own (NotOwned).
	Owned bool
}

// NotOwned reports a paid game the signed-in account does not own: it has
// no Download action, and the page points at the itch.io QR code instead.
func (game DetailGame) NotOwned() bool {
	return !game.IsFree && !game.CanDownload && !game.NeedsSignIn
}

type DetailModel struct {
	State       DetailState
	Game        DetailGame
	Description []string
	Tags        []string
	Images      []string
	ImageIndex  int
	ScrollLine  int
	ScrollMax   int
	BrowserOnly bool
	ErrorDetail string
}

type DetailIntent uint8

const (
	DetailIntentNone DetailIntent = iota
	DetailIntentBack
	DetailIntentSettings
	DetailIntentDownload
	DetailIntentManage
	DetailIntentSignIn
)

func NewDetailModel(game DetailGame) *DetailModel {
	return &DetailModel{State: DetailLoading, Game: game}
}

func (m *DetailModel) SetReady(description string, tags, images []string, browserOnly, warning bool) {
	m.Description = DescriptionParagraphs(description)
	m.Tags = append([]string(nil), tags...)
	m.Images = append([]string(nil), images...)
	m.BrowserOnly = browserOnly
	m.ErrorDetail = ""
	m.State = DetailReady
	if warning {
		m.State = DetailWarning
	}
	m.clampImage()
}

func (m *DetailModel) SetError(detail string) {
	m.State = DetailError
	m.ErrorDetail = detail
}

func (m *DetailModel) SetScrollBounds(maximum int) {
	if maximum < 0 {
		maximum = 0
	}
	m.ScrollMax = maximum
	m.clampScroll()
}

func (m *DetailModel) Handle(event InputEvent) DetailIntent {
	if !event.Pressed {
		return DetailIntentNone
	}
	if event.Button == ButtonB || event.Button == ButtonQuit {
		return DetailIntentBack
	}
	if event.Button == ButtonStart {
		return DetailIntentSettings
	}
	if event.Button == ButtonX && m.Game.Downloaded && (m.State == DetailReady || m.State == DetailError) {
		return DetailIntentManage
	}
	if m.State == DetailError && (event.Button == ButtonUp || event.Button == ButtonDown) {
		if event.Button == ButtonUp {
			m.ScrollLine--
		} else {
			m.ScrollLine++
		}
		m.clampScroll()
	}
	if m.State != DetailReady {
		return DetailIntentNone
	}
	switch event.Button {
	case ButtonLeft, ButtonL1:
		m.ImageIndex--
		m.clampImage()
	case ButtonRight, ButtonR1:
		m.ImageIndex++
		m.clampImage()
	case ButtonUp:
		m.ScrollLine--
		m.clampScroll()
	case ButtonDown:
		m.ScrollLine++
		m.clampScroll()
	case ButtonA:
		if m.Game.CanDownload && !m.BrowserOnly {
			return DetailIntentDownload
		}
		if m.Game.NeedsSignIn && !m.BrowserOnly {
			return DetailIntentSignIn
		}
	}
	return DetailIntentNone
}

func (m *DetailModel) clampImage() {
	if len(m.Images) == 0 {
		m.ImageIndex = 0
		return
	}
	if m.ImageIndex < 0 {
		m.ImageIndex = len(m.Images) - 1
	}
	if m.ImageIndex >= len(m.Images) {
		m.ImageIndex = 0
	}
}

func (m *DetailModel) clampScroll() {
	if m.ScrollLine < 0 {
		m.ScrollLine = 0
	}
	if m.ScrollLine > m.ScrollMax {
		m.ScrollLine = m.ScrollMax
	}
}

// DescriptionParagraphs turns the scraper's small HTML subset into readable,
// renderer-independent paragraphs while preserving Unicode text.
func DescriptionParagraphs(markup string) []string {
	doc, err := html.Parse(strings.NewReader("<body>" + markup + "</body>"))
	if err != nil {
		return nil
	}
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style") {
			return
		}
		if node.Type == html.ElementNode && node.Data == "li" {
			out.WriteString("• ")
		}
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
		}
		if node.Type == html.ElementNode && node.Data == "br" {
			out.WriteByte('\n')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "p", "div", "h1", "h2", "h3", "li", "ul", "ol":
				out.WriteByte('\n')
			}
		}
	}
	walk(doc)
	lines := strings.Split(out.String(), "\n")
	paragraphs := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			paragraphs = append(paragraphs, line)
		}
	}
	return paragraphs
}

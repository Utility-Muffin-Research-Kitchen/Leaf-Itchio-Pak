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
	Owned      bool
	PriceLabel string
}

// PriceText is the price on the detail line: "Owned" for a paid game the
// signed-in account owns, else the current price label. It follows the
// account at draw time, as the page's action does.
func (game DetailGame) PriceText() string {
	if game.Owned && !game.IsFree {
		return "Owned"
	}
	return game.PriceLabel
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
	BodyScroll
	BrowserOnly bool
	ErrorDetail string
	// WarningCategories names the content categories that matched, as the
	// Content Moderation screen names them; the warning screen lists them.
	WarningCategories []string
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

// SetReady shows the loaded page. A non-empty warning, the categories whose
// content filters the game matches, puts the content warning in front of it.
func (m *DetailModel) SetReady(description string, tags, images []string, browserOnly bool, warning []string) {
	m.Description = DescriptionParagraphs(description)
	m.Tags = append([]string(nil), tags...)
	m.Images = append([]string(nil), images...)
	m.BrowserOnly = browserOnly
	m.ErrorDetail = ""
	m.ResetScroll()
	m.State = DetailReady
	m.WarningCategories = nil
	if len(warning) > 0 {
		m.SetWarning(warning)
	}
	m.clampImage()
}

// SetWarning puts the content warning in front of the page, naming the
// categories that matched.
func (m *DetailModel) SetWarning(categories []string) {
	m.State = DetailWarning
	m.WarningCategories = append([]string(nil), categories...)
}

// WarningText is what the content warning says: which of your filters the
// game matches, and where to change them. Start opens Content Moderation
// from the warning. A change applies the next time the game opens.
func WarningText(categories []string) string {
	subject, them := "your content filters", "them"
	switch len(categories) {
	case 0:
	case 1:
		subject, them = "your "+categories[0]+" filter", "it"
	case 2:
		subject = "your " + categories[0] + " and " + categories[1] + " filters"
	default:
		subject = "your " + strings.Join(categories[:len(categories)-1], ", ") + ", and " +
			categories[len(categories)-1] + " filters"
	}
	return "This game matches " + subject + ".\n\nPress Start to change " + them +
		" under Content Moderation, then open the game again. Press B to go back."
}

func (m *DetailModel) SetError(detail string) {
	m.State = DetailError
	m.ErrorDetail = detail
	m.ResetScroll()
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
	if m.State == DetailError {
		m.HandleScroll(event.Button)
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
	case ButtonUp, ButtonDown:
		m.HandleScroll(event.Button)
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

package appui

// BodyBlock is one block of a scrolling body's text: a prose paragraph
// (Text) or a list (Entries). The body puts a blank line between blocks but
// none between the entries of a list, so a prompt that lists a game's thirty
// files takes two lines per file instead of four.
type BodyBlock struct {
	Text    string
	Entries []ListEntry
}

// ListEntry is one item of a list block. Text names it, and Detail, when it
// has any, goes on the line under it: where a file is, or what a rename
// makes of it.
type ListEntry struct{ Text, Detail string }

// IsList reports whether the block is a list rather than a paragraph.
func (b BodyBlock) IsList() bool { return len(b.Entries) > 0 }

// Paragraph is a block of prose.
func Paragraph(text string) BodyBlock { return BodyBlock{Text: text} }

// Prose is one block for each paragraph.
func Prose(paragraphs ...string) []BodyBlock {
	blocks := make([]BodyBlock, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		blocks = append(blocks, Paragraph(paragraph))
	}
	return blocks
}

// ListBlock is a list of entries.
func ListBlock(entries []ListEntry) BodyBlock {
	return BodyBlock{Entries: append([]ListEntry(nil), entries...)}
}

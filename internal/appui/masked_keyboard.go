package appui

import "unicode/utf8"

type MaskedKeyboardIntent uint8

const (
	MaskedKeyboardIntentNone MaskedKeyboardIntent = iota
	MaskedKeyboardIntentAccept
	MaskedKeyboardIntentCancel
)

type MaskedKeyboardModel struct {
	Title    string
	Value    string
	Cursor   int
	Selected int
	Upper    bool
	Maximum  int
}

func NewMaskedKeyboardModel(title, value string) *MaskedKeyboardModel {
	return &MaskedKeyboardModel{Title: title, Value: value, Cursor: len([]rune(value)), Maximum: 128}
}

func (m *MaskedKeyboardModel) Keys() [][]string {
	letters := "abcdefghijklmnopqrstuvwxyz"
	if m.Upper {
		letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	runes := []rune(letters)
	return [][]string{
		stringKeys(runes[:9]), stringKeys(runes[9:18]), stringKeys(runes[18:]),
		{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "-", "_"},
	}
}

func (m *MaskedKeyboardModel) Masked() string {
	count := utf8.RuneCountInString(m.Value)
	if count == 0 {
		return "│ (not entered)"
	}
	masked := make([]rune, count)
	for index := range masked {
		masked[index] = '•'
	}
	cursor := m.Cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(masked) {
		cursor = len(masked)
	}
	return string(masked[:cursor]) + "│" + string(masked[cursor:])
}

func (m *MaskedKeyboardModel) Handle(event InputEvent) MaskedKeyboardIntent {
	if !event.Pressed {
		return MaskedKeyboardIntentNone
	}
	keys := m.Keys()
	positions := flattenKeys(keys)
	if len(positions) == 0 {
		return MaskedKeyboardIntentNone
	}
	switch event.Button {
	case ButtonLeft:
		m.moveHorizontal(keys, -1)
	case ButtonRight:
		m.moveHorizontal(keys, 1)
	case ButtonUp:
		m.moveVertical(keys, -1)
	case ButtonDown:
		m.moveVertical(keys, 1)
	case ButtonA:
		if m.Selected >= 0 && m.Selected < len(positions) && utf8.RuneCountInString(m.Value) < m.Maximum {
			m.insert(positions[m.Selected].value)
		}
	case ButtonB:
		m.backspace()
	case ButtonL1:
		if m.Cursor > 0 {
			m.Cursor--
		}
	case ButtonR1:
		if m.Cursor < utf8.RuneCountInString(m.Value) {
			m.Cursor++
		}
	case ButtonX, ButtonSelect:
		m.Upper = !m.Upper
	case ButtonY, ButtonQuit:
		return MaskedKeyboardIntentCancel
	case ButtonStart:
		if m.Value != "" {
			return MaskedKeyboardIntentAccept
		}
	}
	return MaskedKeyboardIntentNone
}

type keyPosition struct {
	row, column int
	value       string
}

func flattenKeys(keys [][]string) []keyPosition {
	var out []keyPosition
	for row, values := range keys {
		for column, value := range values {
			out = append(out, keyPosition{row: row, column: column, value: value})
		}
	}
	return out
}

func (m *MaskedKeyboardModel) selectedPosition(keys [][]string) (int, int) {
	positions := flattenKeys(keys)
	if m.Selected < 0 || m.Selected >= len(positions) {
		m.Selected = 0
	}
	return positions[m.Selected].row, positions[m.Selected].column
}

func (m *MaskedKeyboardModel) setPosition(keys [][]string, row, column int) {
	if row < 0 {
		row = 0
	}
	if row >= len(keys) {
		row = len(keys) - 1
	}
	if column >= len(keys[row]) {
		column = len(keys[row]) - 1
	}
	if column < 0 {
		column = 0
	}
	index := 0
	for prior := 0; prior < row; prior++ {
		index += len(keys[prior])
	}
	m.Selected = index + column
}

func (m *MaskedKeyboardModel) moveHorizontal(keys [][]string, delta int) {
	row, column := m.selectedPosition(keys)
	column += delta
	if column < 0 {
		column = len(keys[row]) - 1
	}
	if column >= len(keys[row]) {
		column = 0
	}
	m.setPosition(keys, row, column)
}

func (m *MaskedKeyboardModel) moveVertical(keys [][]string, delta int) {
	row, column := m.selectedPosition(keys)
	row += delta
	if row < 0 {
		row = len(keys) - 1
	}
	if row >= len(keys) {
		row = 0
	}
	m.setPosition(keys, row, column)
}

func (m *MaskedKeyboardModel) insert(value string) {
	runes := []rune(m.Value)
	insert := []rune(value)
	updated := make([]rune, 0, len(runes)+len(insert))
	updated = append(updated, runes[:m.Cursor]...)
	updated = append(updated, insert...)
	updated = append(updated, runes[m.Cursor:]...)
	m.Cursor += len(insert)
	m.Value = string(updated)
}

func (m *MaskedKeyboardModel) backspace() {
	if m.Cursor <= 0 {
		return
	}
	runes := []rune(m.Value)
	runes = append(runes[:m.Cursor-1], runes[m.Cursor:]...)
	m.Cursor--
	m.Value = string(runes)
}

func stringKeys(values []rune) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = string(value)
	}
	return out
}

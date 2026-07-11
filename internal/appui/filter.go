package appui

// FilterSection identifies the editable row in the filter screen.
type FilterSection uint8

const (
	FilterSearch FilterSection = iota
	FilterPlatform
	FilterSort
)

var FilterPlatforms = []string{"", "GB", "GBC", "GBA", "NES", "MD", "P8"}
var FilterPlatformLabels = []string{"All platforms", "GB", "GBC", "GBA", "NES", "Mega Drive", "Pico-8"}
var FilterSortValues = []string{"", "az", "za", "new", "free", "paid", "dl", "owned"}
var FilterSortLabels = []string{"RSS", "A-Z", "Z-A", "Newest", "Free", "Paid", "Downloaded", "Owned"}

type FilterIntent uint8

const (
	FilterIntentNone FilterIntent = iota
	FilterIntentEditSearch
	FilterIntentApply
	FilterIntentCancel
)

// FilterModel owns a staged filter edit. Apply is the only intent that commits
// its values to the list; Cancel leaves the active list unchanged.
type FilterModel struct {
	Section  FilterSection
	Platform string
	Sort     string
	Query    string
	plat     int
	sort     int
}

func NewFilterModel(platform, sort, query string) *FilterModel {
	m := &FilterModel{Platform: platform, Sort: sort, Query: query}
	m.plat = indexOf(FilterPlatforms, platform)
	m.sort = indexOf(FilterSortValues, sort)
	return m
}

func (m *FilterModel) PlatformLabel() string { return FilterPlatformLabels[m.plat] }
func (m *FilterModel) SortLabel() string     { return FilterSortLabels[m.sort] }

func (m *FilterModel) Clear() {
	m.Platform, m.Sort, m.Query = "", "", ""
	m.plat, m.sort = 0, 0
}

func (m *FilterModel) Handle(event InputEvent) FilterIntent {
	if !event.Pressed {
		return FilterIntentNone
	}
	switch event.Button {
	case ButtonUp:
		if m.Section > FilterSearch {
			m.Section--
		}
	case ButtonDown:
		if m.Section < FilterSort {
			m.Section++
		}
	case ButtonLeft:
		m.cycle(-1)
	case ButtonRight:
		m.cycle(1)
	case ButtonA:
		if m.Section == FilterSearch {
			return FilterIntentEditSearch
		}
		m.cycle(1)
	case ButtonB, ButtonQuit:
		return FilterIntentCancel
	case ButtonSelect:
		return FilterIntentApply
	case ButtonY:
		m.Clear()
	}
	return FilterIntentNone
}

func (m *FilterModel) cycle(delta int) {
	switch m.Section {
	case FilterPlatform:
		m.plat = wrap(m.plat+delta, len(FilterPlatforms))
		m.Platform = FilterPlatforms[m.plat]
	case FilterSort:
		m.sort = wrap(m.sort+delta, len(FilterSortValues))
		m.Sort = FilterSortValues[m.sort]
	}
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return 0
}

func wrap(value, length int) int {
	if length == 0 {
		return 0
	}
	value %= length
	if value < 0 {
		value += length
	}
	return value
}

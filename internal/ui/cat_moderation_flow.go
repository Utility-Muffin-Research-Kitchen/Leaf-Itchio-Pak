//go:build !headless

package ui

import (
	"fmt"
	"slices"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type CatModerationFlow struct {
	cfg     *settings.Config
	cfgPath string
}

func NewCatModerationFlow(cfg *settings.Config, cfgPath string) (*CatModerationFlow, *appui.SettingsModel) {
	flow := &CatModerationFlow{cfg: cfg, cfgPath: cfgPath}
	model := appui.NewSettingsModel("Content Moderation")
	flow.Refresh(model)
	return flow, model
}

func (flow *CatModerationFlow) Refresh(model *appui.SettingsModel) {
	model.SetRows("Local advisory filters · creator tagging may be incomplete", []appui.SettingsRow{
		{Key: appui.SettingsAdultContent, Label: "Adult Content", Value: categoryValue(flow.cfg.Filter.AdultContent, itchio.AdultContentTags), ActionEnabled: true},
		{Key: appui.SettingsQueerContent, Label: "Queer Content", Value: categoryValue(flow.cfg.Filter.QueerContent, itchio.QueerContentTags), ActionEnabled: true},
		{Key: appui.SettingsHeavyThemes, Label: "Heavy Themes", Value: categoryValue(flow.cfg.Filter.HeavyThemes, itchio.HeavyThemesTags), ActionEnabled: true},
		{Key: appui.SettingsSubstanceUse, Label: "Substance Use", Value: blockedAllowed(flow.cfg.Filter.SubstanceUse.Enabled), ActionEnabled: true},
	})
}

func (flow *CatModerationFlow) Activate(model *appui.SettingsModel) (*CatTagFlow, *appui.SettingsModel, error) {
	row, ok := model.Selected()
	if !ok {
		return nil, nil, nil
	}
	switch row.Key {
	case appui.SettingsAdultContent:
		return newCatTagFlow(flow.cfg, flow.cfgPath, "Adult Content", itchio.AdultContentTags, &flow.cfg.Filter.AdultContent)
	case appui.SettingsQueerContent:
		return newCatTagFlow(flow.cfg, flow.cfgPath, "Queer Content", itchio.QueerContentTags, &flow.cfg.Filter.QueerContent)
	case appui.SettingsHeavyThemes:
		return newCatTagFlow(flow.cfg, flow.cfgPath, "Heavy Themes", itchio.HeavyThemesTags, &flow.cfg.Filter.HeavyThemes)
	case appui.SettingsSubstanceUse:
		flow.cfg.Filter.SubstanceUse.Enabled = !flow.cfg.Filter.SubstanceUse.Enabled
		if err := flow.cfg.Save(flow.cfgPath); err != nil {
			return nil, nil, err
		}
		flow.Refresh(model)
	}
	return nil, nil, nil
}

type CatTagFlow struct {
	cfg      *settings.Config
	cfgPath  string
	title    string
	tags     []string
	category *settings.CategoryFilter
}

func newCatTagFlow(cfg *settings.Config, cfgPath, title string, tags []string,
	category *settings.CategoryFilter) (*CatTagFlow, *appui.SettingsModel, error) {
	flow := &CatTagFlow{cfg: cfg, cfgPath: cfgPath, title: title, tags: append([]string(nil), tags...), category: category}
	model := appui.NewSettingsModel(title)
	flow.Refresh(model)
	return flow, model, nil
}

func (flow *CatTagFlow) Refresh(model *appui.SettingsModel) {
	rows := []appui.SettingsRow{{
		Key: appui.SettingsTagMaster, Label: "All category tags", Value: blockedAllowed(flow.category.Enabled), ActionEnabled: true,
	}}
	for index, tag := range flow.tags {
		blocked := flow.category.Enabled && !slices.Contains(flow.category.Disabled, tag)
		rows = append(rows, appui.SettingsRow{
			Key: appui.SettingsTag, Label: tag, Value: blockedAllowed(blocked), Index: index, ActionEnabled: true,
		})
	}
	model.SetRows("A toggles · category coverage depends on creator tags", rows)
}

func (flow *CatTagFlow) Activate(model *appui.SettingsModel) error {
	row, ok := model.Selected()
	if !ok {
		return nil
	}
	switch row.Key {
	case appui.SettingsTagMaster:
		flow.category.Enabled = !flow.category.Enabled
	case appui.SettingsTag:
		if row.Index < 0 || row.Index >= len(flow.tags) {
			return fmt.Errorf("content tag changed before toggle")
		}
		tag := flow.tags[row.Index]
		if slices.Contains(flow.category.Disabled, tag) {
			flow.category.Disabled = removeString(flow.category.Disabled, tag)
		} else {
			flow.category.Disabled = append(flow.category.Disabled, tag)
		}
	}
	if err := flow.cfg.Save(flow.cfgPath); err != nil {
		return err
	}
	flow.Refresh(model)
	return nil
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func categoryValue(category settings.CategoryFilter, tags []string) string {
	if !category.Enabled {
		return "Allowed >"
	}
	blocked := 0
	for _, tag := range tags {
		if !slices.Contains(category.Disabled, tag) {
			blocked++
		}
	}
	return fmt.Sprintf("%d blocked >", blocked)
}

func blockedAllowed(blocked bool) string {
	if blocked {
		return "Blocked"
	}
	return "Allowed"
}

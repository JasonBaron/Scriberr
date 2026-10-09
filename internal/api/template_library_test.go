package api

import (
	"strings"
	"testing"
	"time"

	"scriberr/internal/models"
	"scriberr/internal/titles"
)

// Every auto tag in the library must be a tag in the default vocabulary,
// or the template would never run automatically.
func TestTemplateLibraryAutoTagsInVocabulary(t *testing.T) {
	v := titles.NewVocabulary("", "", "")
	names := map[string]bool{}
	for _, lib := range templateLibrary {
		if names[lib.Name] {
			t.Errorf("duplicate library name %q", lib.Name)
		}
		names[lib.Name] = true
		if !strings.Contains(lib.Prompt, "{me}") {
			t.Errorf("%s: prompt does not use {me}", lib.Name)
		}
		for _, tag := range lib.AutoTags {
			if v.ResolveType(tag) != tag && v.ResolveTopic(tag) != tag {
				t.Errorf("%s: auto tag %q is not in the default vocabulary", lib.Name, tag)
			}
		}
	}
}

func TestNewestSummary(t *testing.T) {
	tpl := "default"
	other := "other"
	now := time.Now()
	sums := []models.Summary{
		{ID: "a", TemplateID: &tpl, CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "b", TemplateID: &other, CreatedAt: now},
		{ID: "c", TemplateID: &tpl, CreatedAt: now.Add(-1 * time.Hour)},
	}
	if got := newestSummary(sums, tpl).ID; got != "c" {
		t.Errorf("got %s, want newest default-template summary c", got)
	}
	if got := newestSummary(sums, "missing").ID; got != "b" {
		t.Errorf("got %s, want newest overall b", got)
	}
}

func TestLibraryModel(t *testing.T) {
	items := []models.SummaryTemplate{{Name: "A", Model: "llama3"}, {Name: "B", Model: "qwen3:8b"}, {Name: "C", Model: "qwen3:8b"}, {Name: "D", IsDefault: true}}
	if m, def := libraryModel(items, ""); m != "qwen3:8b" || !def {
		t.Errorf("most used: %q %v", m, def)
	}
	if m, _ := libraryModel(items, "gemma"); m != "gemma" {
		t.Errorf("settings model: %q", m)
	}
	items[3].Model = "mistral"
	if m, _ := libraryModel(items, "gemma"); m != "mistral" {
		t.Errorf("default template model: %q", m)
	}
	if m, def := libraryModel(nil, ""); m != "" || def {
		t.Errorf("empty: %q %v", m, def)
	}
}

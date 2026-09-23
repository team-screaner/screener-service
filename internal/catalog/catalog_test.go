package catalog

import (
	"strings"
	"testing"
)

func TestLoadCompleteSeed(t *testing.T) {
	t.Parallel()
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Templates) != 8 {
		t.Fatalf("templates = %d, want 8", len(got.Templates))
	}
	if len(got.FactTypes) != 24 {
		t.Fatalf("fact types = %d, want 24", len(got.FactTypes))
	}
	counts := map[string]int{}
	for _, skill := range got.Skills {
		counts[skill.Category]++
	}
	for category, minimum := range map[string]int{"backend": 50, "qa": 40, "devops_sre": 40, "frontend_fullstack": 40, "analytics": 40, "engineering_core": 10} {
		if counts[category] < minimum {
			t.Errorf("%s has %d skills, want >= %d", category, counts[category], minimum)
		}
	}
	for _, template := range got.Templates {
		if len(template.SkillKeys) < 20 || len(template.Levels) < 3 || len(template.Requirements) < 10 {
			t.Errorf("incomplete template %s", template.Key)
		}
	}
}

func TestLoadReturnsIndependentValues(t *testing.T) {
	t.Parallel()
	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	first.Skills[0].Signals[0] = "corrupted"
	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if second.Skills[0].Signals[0] == "corrupted" {
		t.Fatal("loads share mutable memory")
	}
}

func TestValidateRejectsBrokenCatalog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Catalog)
		message string
	}{
		{"duplicate skill", func(c *Catalog) { c.Skills = append(c.Skills, c.Skills[0]) }, "duplicate skill"},
		{"empty name", func(c *Catalog) { c.Skills[0].Name = "  " }, "skill"},
		{"empty signal", func(c *Catalog) { c.Skills[0].Signals[0] = "" }, "signal"},
		{"missing signal", func(c *Catalog) { c.Skills[0].Signals = nil }, "signal"},
		{"duplicate fact", func(c *Catalog) { c.FactTypes = append(c.FactTypes, c.FactTypes[0]) }, "duplicate fact type"},
		{"duplicate template", func(c *Catalog) { c.Templates = append(c.Templates, c.Templates[0]) }, "duplicate template"},
		{"unknown skill", func(c *Catalog) { c.Templates[0].SkillKeys[0] = "missing" }, "unknown skill"},
		{"duplicate level", func(c *Catalog) { c.Templates[0].Levels = append(c.Templates[0].Levels, c.Templates[0].Levels[0]) }, "duplicate level"},
		{"unknown requirement level", func(c *Catalog) { c.Templates[0].Requirements[0].LevelKey = "missing" }, "unknown level"},
		{"unknown requirement skill", func(c *Catalog) { c.Templates[0].Requirements[0].SkillKey = "missing" }, "unknown skill"},
		{"requirement outside template", func(c *Catalog) { c.Templates[0].SkillKeys = c.Templates[0].SkillKeys[1:] }, "outside template"},
		{"duplicate requirement", func(c *Catalog) {
			c.Templates[0].Requirements = append(c.Templates[0].Requirements, c.Templates[0].Requirements[0])
		}, "duplicate requirement"},
		{"empty requirement", func(c *Catalog) { c.Templates[0].Requirements[0].Description = "" }, "description"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(&c)
			err = c.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("error = %v, want %q", err, tt.message)
			}
		})
	}
}

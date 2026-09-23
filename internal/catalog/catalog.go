// Package catalog loads the editable, versioned screening seed shipped with the service.
package catalog

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed seed.json
var seed []byte

// Skill describes a reusable competency and observable evidence signals.
type Skill struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Signals     []string `json:"signals"`
}

// FactType names a supported category of professional evidence.
type FactType struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Level defines a configurable progression stage within a template.
type Level struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Requirement describes a skill expectation at a particular level.
type Requirement struct {
	SkillKey    string `json:"skill_key"`
	LevelKey    string `json:"level_key"`
	Description string `json:"description"`
	Critical    bool   `json:"critical"`
}

// Template selects competencies and progression requirements for a role.
type Template struct {
	Key          string        `json:"key"`
	Name         string        `json:"name"`
	SkillKeys    []string      `json:"skill_keys"`
	Levels       []Level       `json:"levels"`
	Requirements []Requirement `json:"requirements"`
}

// Catalog contains the editable system dictionaries and starting templates.
type Catalog struct {
	Skills    []Skill    `json:"skills"`
	FactTypes []FactType `json:"fact_types"`
	Templates []Template `json:"templates"`
}

// Load returns a fresh validated copy, so callers cannot mutate the embedded catalog.
func Load() (Catalog, error) {
	var c Catalog
	decoder := json.NewDecoder(bytes.NewReader(seed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Catalog{}, fmt.Errorf("validate catalog: %w", err)
	}
	return c, nil
}

// Validate checks data integrity without encoding any profession or level in Go.
func (c Catalog) Validate() error {
	if len(c.Skills) == 0 || len(c.FactTypes) == 0 || len(c.Templates) == 0 {
		return errors.New("catalog skills, fact types and templates are required")
	}
	skills := make(map[string]bool, len(c.Skills))
	for _, skill := range c.Skills {
		if err := unique(skills, skill.Key, "skill"); err != nil {
			return err
		}
		if blank(skill.Name) || blank(skill.Description) || blank(skill.Category) {
			return fmt.Errorf("skill %q requires name, description and category", skill.Key)
		}
		if len(skill.Signals) == 0 {
			return fmt.Errorf("skill %q requires signals", skill.Key)
		}
		for _, signal := range skill.Signals {
			if blank(signal) {
				return fmt.Errorf("skill %q has empty signal", skill.Key)
			}
		}
	}
	facts := make(map[string]bool, len(c.FactTypes))
	for _, fact := range c.FactTypes {
		if err := unique(facts, fact.Key, "fact type"); err != nil {
			return err
		}
		if blank(fact.Name) {
			return fmt.Errorf("fact type %q requires name", fact.Key)
		}
	}
	templates := make(map[string]bool, len(c.Templates))
	for _, template := range c.Templates {
		if err := unique(templates, template.Key, "template"); err != nil {
			return err
		}
		if err := validateTemplate(template, skills); err != nil {
			return fmt.Errorf("template %q: %w", template.Key, err)
		}
	}
	return nil
}
func validateTemplate(template Template, skills map[string]bool) error {
	if blank(template.Name) || len(template.SkillKeys) == 0 || len(template.Levels) == 0 || len(template.Requirements) == 0 {
		return errors.New("name, skills, levels and requirements are required")
	}
	selected := make(map[string]bool, len(template.SkillKeys))
	for _, key := range template.SkillKeys {
		if !skills[key] {
			return fmt.Errorf("unknown skill %q", key)
		}
		if err := unique(selected, key, "selected skill"); err != nil {
			return err
		}
	}
	levels := make(map[string]bool, len(template.Levels))
	for _, level := range template.Levels {
		if err := unique(levels, level.Key, "level"); err != nil {
			return err
		}
		if blank(level.Name) || blank(level.Description) {
			return fmt.Errorf("level %q requires name and description", level.Key)
		}
	}
	requirements := make(map[string]bool, len(template.Requirements))
	for _, requirement := range template.Requirements {
		if !skills[requirement.SkillKey] {
			return fmt.Errorf("requirement references unknown skill %q", requirement.SkillKey)
		}
		if !selected[requirement.SkillKey] {
			return fmt.Errorf("requirement skill %q is outside template", requirement.SkillKey)
		}
		if !levels[requirement.LevelKey] {
			return fmt.Errorf("requirement references unknown level %q", requirement.LevelKey)
		}
		if blank(requirement.Description) {
			return fmt.Errorf("requirement %q requires description", requirement.SkillKey)
		}
		if err := unique(requirements, requirement.SkillKey+"/"+requirement.LevelKey, "requirement"); err != nil {
			return err
		}
	}
	return nil
}
func unique(seen map[string]bool, key, kind string) error {
	if blank(key) {
		return fmt.Errorf("%s key is required", kind)
	}
	if seen[key] {
		return fmt.Errorf("duplicate %s %q", kind, key)
	}
	seen[key] = true
	return nil
}
func blank(value string) bool { return strings.TrimSpace(value) == "" }

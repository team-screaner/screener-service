// Package domain defines infrastructure-independent assessment policy and service contracts.
package domain

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ErrInvalidReadiness indicates malformed assessment data or hierarchy.
var ErrInvalidReadiness = errors.New("invalid readiness input")

// RequirementProgress is the assessment of a requirement in its skill and group.
// A missing assessment uses the zero score. Scores range from zero to four.
type RequirementProgress struct {
	RequirementID, SkillID, GroupID             string
	RequirementWeight, SkillWeight, GroupWeight float64
	Required, Critical, NotApplicable           bool
	NAReason                                    string
	Score                                       float64
}

// ReadinessResult contains the weighted assessment and unresolved requirements.
type ReadinessResult struct {
	Percent      float64
	Ready        bool
	CriticalGaps []string
	Gaps         []Gap
	Groups       []GroupProgress
}

// Gap identifies an applicable requirement with a score below four.
type Gap struct {
	RequirementID, SkillID, GroupID string
	Score                           float64
	Required, Critical              bool
}

// GroupProgress is the percentage for one applicable group.
type GroupProgress struct {
	GroupID string
	Percent float64
}

// CalculateReadiness evaluates progress without mutating its input.
func CalculateReadiness(progress []RequirementProgress) (ReadinessResult, error) {
	if err := validateReadiness(progress); err != nil {
		return ReadinessResult{}, err
	}
	var result ReadinessResult
	groups := make(map[string]*readinessGroup)
	blocked := false
	for _, p := range progress {
		if p.NotApplicable {
			continue
		}
		group := groups[p.GroupID]
		if group == nil {
			group = &readinessGroup{weight: p.GroupWeight, skills: make(map[string]*readinessSkill)}
			groups[p.GroupID] = group
		}
		skill := group.skills[p.SkillID]
		if skill == nil {
			skill = &readinessSkill{weight: p.SkillWeight}
			group.skills[p.SkillID] = skill
		}
		skill.scores = append(skill.scores, weightedScore{value: p.Score * 25, weight: p.RequirementWeight})
		blocked = blocked || (p.Score < 4 && (p.Required || p.Critical))
		if p.Score < 4 {
			result.Gaps = append(result.Gaps, Gap{RequirementID: p.RequirementID, SkillID: p.SkillID, GroupID: p.GroupID, Score: p.Score, Required: p.Required, Critical: p.Critical})
			if p.Critical {
				result.CriticalGaps = append(result.CriticalGaps, p.RequirementID)
			}
		}
	}
	var groupScores []weightedScore
	for _, id := range sortedReadinessKeys(groups) {
		group := groups[id]
		var skillScores []weightedScore
		for _, skillID := range sortedReadinessKeys(group.skills) {
			skill := group.skills[skillID]
			skillScores = append(skillScores, weightedScore{value: readinessMean(skill.scores), weight: skill.weight})
		}
		percent := readinessMean(skillScores)
		groupScores = append(groupScores, weightedScore{value: percent, weight: group.weight})
		result.Groups = append(result.Groups, GroupProgress{GroupID: id, Percent: percent})
	}
	sort.Strings(result.CriticalGaps)
	sort.Slice(result.Gaps, func(i, j int) bool { return result.Gaps[i].RequirementID < result.Gaps[j].RequirementID })
	result.Percent = readinessMean(groupScores)
	result.Ready = len(groupScores) > 0 && result.Percent >= 80 && !blocked
	return result, nil
}

type readinessSkill struct {
	weight float64
	scores []weightedScore
}

type readinessGroup struct {
	weight float64
	skills map[string]*readinessSkill
}

type weightedScore struct{ value, weight float64 }

func readinessMean(scores []weightedScore) float64 {
	// Normalize first so finite weights near MaxFloat64 cannot overflow sums.
	var maxWeight float64
	for _, score := range scores {
		maxWeight = max(maxWeight, score.weight)
	}
	if maxWeight == 0 {
		return 0
	}
	var total, weight float64
	for _, score := range scores {
		normalized := score.weight / maxWeight
		total += score.value * normalized
		weight += normalized
	}
	if weight == 0 {
		return 0
	}
	return total / weight
}

func validateReadiness(progress []RequirementProgress) error {
	requirements := make(map[string]struct{}, len(progress))
	skills := make(map[string]RequirementProgress)
	groups := make(map[string]float64)
	for _, p := range progress {
		if strings.TrimSpace(p.RequirementID) == "" || strings.TrimSpace(p.SkillID) == "" || strings.TrimSpace(p.GroupID) == "" {
			return fmt.Errorf("%w: requirement, skill, and group IDs are required", ErrInvalidReadiness)
		}
		if !finitePositiveWeight(p.RequirementWeight) || !finitePositiveWeight(p.SkillWeight) || !finitePositiveWeight(p.GroupWeight) {
			return fmt.Errorf("%w: requirement %q has invalid weights", ErrInvalidReadiness, p.RequirementID)
		}
		if math.IsNaN(p.Score) || math.IsInf(p.Score, 0) || p.Score < 0 || p.Score > 4 {
			return fmt.Errorf("%w: requirement %q score must be finite and in [0,4]", ErrInvalidReadiness, p.RequirementID)
		}
		if p.NotApplicable && strings.TrimSpace(p.NAReason) == "" {
			return fmt.Errorf("%w: requirement %q needs an N/A reason", ErrInvalidReadiness, p.RequirementID)
		}
		if _, exists := requirements[p.RequirementID]; exists {
			return fmt.Errorf("%w: duplicate requirement %q", ErrInvalidReadiness, p.RequirementID)
		}
		if skill, exists := skills[p.SkillID]; exists && (skill.SkillWeight != p.SkillWeight || skill.GroupID != p.GroupID) {
			return fmt.Errorf("%w: inconsistent skill %q", ErrInvalidReadiness, p.SkillID)
		}
		if weight, exists := groups[p.GroupID]; exists && weight != p.GroupWeight {
			return fmt.Errorf("%w: inconsistent group %q", ErrInvalidReadiness, p.GroupID)
		}
		requirements[p.RequirementID] = struct{}{}
		skills[p.SkillID] = p
		groups[p.GroupID] = p.GroupWeight
	}
	return nil
}

func finitePositiveWeight(weight float64) bool {
	return weight > 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0)
}

func sortedReadinessKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

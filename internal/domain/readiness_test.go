package domain

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestCalculateReadinessEmpty(t *testing.T) {
	t.Parallel()
	got, err := CalculateReadiness(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Percent != 0 || got.Ready {
		t.Fatalf("empty assessment = %+v, want zero and not ready", got)
	}
}

func TestCalculateReadinessNotApplicable(t *testing.T) {
	t.Parallel()
	na := RequirementProgress{RequirementID: "na", SkillID: "s", GroupID: "g", RequirementWeight: 9, SkillWeight: 1, GroupWeight: 1, Required: true, Critical: true, NotApplicable: true, NAReason: "Out of scope"}
	for _, tt := range []struct {
		name    string
		input   []RequirementProgress
		percent float64
		ready   bool
	}{
		{name: "all excluded", input: []RequirementProgress{na}},
		{name: "excluded from denominator and blockers", input: []RequirementProgress{na, {RequirementID: "done", SkillID: "s", GroupID: "g", RequirementWeight: 1, SkillWeight: 1, GroupWeight: 1, Score: 4}}, percent: 100, ready: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := CalculateReadiness(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Percent != tt.percent || got.Ready != tt.ready || len(got.Gaps) != 0 || len(got.CriticalGaps) != 0 {
				t.Fatalf("result = %+v, want percent %v ready %v and no gaps", got, tt.percent, tt.ready)
			}
		})
	}
}

func TestCalculateReadinessBreakdown(t *testing.T) {
	t.Parallel()
	input := []RequirementProgress{
		{RequirementID: "z", SkillID: "s2", GroupID: "g2", RequirementWeight: 1, SkillWeight: 1, GroupWeight: 1, Score: 2, Critical: true},
		{RequirementID: "b", SkillID: "s1", GroupID: "g1", RequirementWeight: 1, SkillWeight: 1, GroupWeight: 1, Score: 4},
		{RequirementID: "a", SkillID: "s1", GroupID: "g1", RequirementWeight: 1, SkillWeight: 1, GroupWeight: 1, Required: true, Critical: true},
	}
	before := append([]RequirementProgress(nil), input...)
	got, err := CalculateReadiness(input)
	if err != nil {
		t.Fatal(err)
	}
	want := ReadinessResult{
		Percent: 50, CriticalGaps: []string{"a", "z"},
		Gaps: []Gap{
			{RequirementID: "a", SkillID: "s1", GroupID: "g1", Required: true, Critical: true},
			{RequirementID: "z", SkillID: "s2", GroupID: "g2", Score: 2, Critical: true},
		},
		Groups: []GroupProgress{{GroupID: "g1", Percent: 50}, {GroupID: "g2", Percent: 50}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("mutated input")
	}
}

func TestCalculateReadinessNestedWeights(t *testing.T) {
	t.Parallel()
	input := []RequirementProgress{
		{RequirementID: "a", SkillID: "s1", GroupID: "g1", RequirementWeight: 1, SkillWeight: 1, GroupWeight: 1, Score: 4},
		{RequirementID: "b", SkillID: "s1", GroupID: "g1", RequirementWeight: 3, SkillWeight: 1, GroupWeight: 1, Score: 0},
		{RequirementID: "c", SkillID: "s2", GroupID: "g1", RequirementWeight: 1, SkillWeight: 3, GroupWeight: 1, Score: 4},
		{RequirementID: "d", SkillID: "s3", GroupID: "g2", RequirementWeight: 1, SkillWeight: 1, GroupWeight: 3, Score: 2},
	}
	got, err := CalculateReadiness(input)
	if err != nil {
		t.Fatal(err)
	}
	// s1 = 25%, s2 = 100%, g1 = 81.25%; g2 = 50%; total = 57.8125%.
	if math.Abs(got.Percent-57.8125) > 1e-9 || got.Ready {
		t.Fatalf("result = %+v, want 57.8125%% and not ready", got)
	}
}

func TestCalculateReadinessReadyPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                      string
		score                     float64
		required, critical, ready bool
	}{
		{name: "complete", score: 4, required: true, critical: true, ready: true},
		{name: "threshold inclusive", score: 3.2, ready: true},
		{name: "below threshold", score: 3.19},
		{name: "required gap", score: 3.9, required: true},
		{name: "critical gap", score: 3.9, critical: true},
		{name: "missing score"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := CalculateReadiness([]RequirementProgress{{
				RequirementID: "r", SkillID: "s", GroupID: "g", RequirementWeight: 1,
				SkillWeight: 1, GroupWeight: 1, Score: tt.score, Required: tt.required, Critical: tt.critical,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Ready != tt.ready {
				t.Fatalf("ready = %v, want %v", got.Ready, tt.ready)
			}
		})
	}
}

func TestCalculateReadinessRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	valid := RequirementProgress{RequirementID: "r", SkillID: "s", GroupID: "g", RequirementWeight: 1, SkillWeight: 1, GroupWeight: 1, Score: 4}
	tests := []struct {
		name   string
		change func(*RequirementProgress)
	}{
		{"empty requirement id", func(p *RequirementProgress) { p.RequirementID = " " }},
		{"empty skill id", func(p *RequirementProgress) { p.SkillID = "" }},
		{"empty group id", func(p *RequirementProgress) { p.GroupID = "" }},
		{"negative score", func(p *RequirementProgress) { p.Score = -1 }},
		{"large score", func(p *RequirementProgress) { p.Score = 4.1 }},
		{"nan score", func(p *RequirementProgress) { p.Score = math.NaN() }},
		{"infinite score", func(p *RequirementProgress) { p.Score = math.Inf(1) }},
		{"zero requirement weight", func(p *RequirementProgress) { p.RequirementWeight = 0 }},
		{"negative requirement weight", func(p *RequirementProgress) { p.RequirementWeight = -1 }},
		{"nan requirement weight", func(p *RequirementProgress) { p.RequirementWeight = math.NaN() }},
		{"infinite requirement weight", func(p *RequirementProgress) { p.RequirementWeight = math.Inf(1) }},
		{"zero skill weight", func(p *RequirementProgress) { p.SkillWeight = 0 }},
		{"negative skill weight", func(p *RequirementProgress) { p.SkillWeight = -1 }},
		{"nan skill weight", func(p *RequirementProgress) { p.SkillWeight = math.NaN() }},
		{"infinite skill weight", func(p *RequirementProgress) { p.SkillWeight = math.Inf(-1) }},
		{"zero group weight", func(p *RequirementProgress) { p.GroupWeight = 0 }},
		{"negative group weight", func(p *RequirementProgress) { p.GroupWeight = -1 }},
		{"nan group weight", func(p *RequirementProgress) { p.GroupWeight = math.NaN() }},
		{"infinite group weight", func(p *RequirementProgress) { p.GroupWeight = math.Inf(1) }},
		{"na without reason", func(p *RequirementProgress) { p.NotApplicable = true; p.NAReason = " \n " }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := valid
			tt.change(&input)
			got, err := CalculateReadiness([]RequirementProgress{input})
			if !errors.Is(err, ErrInvalidReadiness) {
				t.Fatalf("invalid input error = %v, want ErrInvalidReadiness: %+v", err, input)
			}
			if !reflect.DeepEqual(got, ReadinessResult{}) {
				t.Fatalf("returned partial result on error: %+v", got)
			}
		})
	}
}

func TestCalculateReadinessRejectsAmbiguousHierarchy(t *testing.T) {
	t.Parallel()
	base := RequirementProgress{RequirementID: "r", SkillID: "s", GroupID: "g", RequirementWeight: 1, SkillWeight: 1, GroupWeight: 1, Score: 4}
	for _, tt := range []struct {
		name   string
		change func(*RequirementProgress)
	}{
		{"duplicate requirement", func(p *RequirementProgress) { p.RequirementID = "r" }},
		{"conflicting skill weights", func(p *RequirementProgress) { p.SkillWeight = 2 }},
		{"conflicting group weights", func(p *RequirementProgress) { p.GroupWeight = 2 }},
		{"skill in two groups", func(p *RequirementProgress) { p.GroupID = "g2" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			other := base
			other.RequirementID = "r2"
			tt.change(&other)
			if _, err := CalculateReadiness([]RequirementProgress{base, other}); err == nil {
				t.Fatal("accepted ambiguous hierarchy")
			}
		})
	}
}

func TestCalculateReadinessLargeFiniteWeights(t *testing.T) {
	t.Parallel()
	input := []RequirementProgress{
		{RequirementID: "a", SkillID: "s1", GroupID: "g1", RequirementWeight: math.MaxFloat64, SkillWeight: math.MaxFloat64, GroupWeight: math.MaxFloat64, Score: 4},
		{RequirementID: "b", SkillID: "s1", GroupID: "g1", RequirementWeight: math.MaxFloat64, SkillWeight: math.MaxFloat64, GroupWeight: math.MaxFloat64, Score: 0},
		{RequirementID: "c", SkillID: "s2", GroupID: "g1", RequirementWeight: math.MaxFloat64, SkillWeight: math.MaxFloat64, GroupWeight: math.MaxFloat64, Score: 4},
		{RequirementID: "d", SkillID: "s3", GroupID: "g2", RequirementWeight: math.MaxFloat64, SkillWeight: math.MaxFloat64, GroupWeight: math.MaxFloat64, Score: 1},
	}
	got, err := CalculateReadiness(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Percent != 50 {
		t.Fatalf("percent = %v, want 50", got.Percent)
	}
}

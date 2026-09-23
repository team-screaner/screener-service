// Package usecase enforces caller capabilities before repository access.
package usecase

import (
	"context"
	"slices"

	"github.com/team-screaner/screener-service/internal/domain"
)

// Service enforces token capabilities before any repository access or replay.
type Service struct{ backend domain.Backend }

// New constructs a service using its persistence boundary.
func New(b domain.Backend) *Service { return &Service{backend: b} }

// Authenticate verifies a bearer token through the configured backend.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.Actor, error) {
	return s.backend.Authenticate(ctx, token)
}

// Execute checks agent scopes before invoking a domain operation.
func (s *Service) Execute(ctx context.Context, c domain.Command) (domain.Result, error) {
	if c.Actor.Kind == "agent" {
		var scope string
		switch c.Operation {
		case "getMe":
			scope = "matrix:read"
		case "listMatrices", "getMatrix", "listVersions", "getVersion", "listSkills", "listFactTypes", "listUserMatrices":
			scope = "matrix:read"
		case "getGrowthContext":
			if !slices.Contains(c.Actor.Scopes, "evidence:read") {
				return nil, domain.Forbidden()
			}
			scope = "matrix:read"
		case "listEvidence":
			scope = "evidence:read"
		case "createEvidence", "batchEvidence":
			scope = "evidence:write"
		case "listAssessments", "getAssessment", "getGaps", "getRadar":
			scope = "assessment:read"
		default:
			return nil, domain.Forbidden()
		}
		if !slices.Contains(c.Actor.Scopes, scope) {
			return nil, domain.Forbidden()
		}
	}
	return s.backend.Execute(ctx, c)
}

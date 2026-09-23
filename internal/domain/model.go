package domain

import (
	"context"
	"encoding/json"
	"net/url"
)

// Actor is authenticated before commands, including retained-response replay.
type Actor struct {
	UserID, TokenID, Kind string
	Scopes                []string
}

// Command carries an authenticated operation and its request data.
type Command struct {
	Operation, Method, Path, ID, Key string
	Query                            url.Values
	Body                             json.RawMessage
	Actor                            Actor
}

// Result contains the structured response for a completed operation.
type Result map[string]any

// Error describes a stable public error independent of infrastructure.
type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string { return e.Message }

// Invalid reports invalid caller input.
func Invalid(message string) error { return &Error{400, "invalid_input", message} }

// Forbidden reports an operation outside the caller's permissions.
func Forbidden() error { return &Error{403, "forbidden", "Operation is not permitted"} }

// NotFound reports a missing or inaccessible resource.
func NotFound() error { return &Error{404, "not_found", "Resource not found"} }

// Conflict reports incompatible concurrent or existing state.
func Conflict(message string) error { return &Error{409, "conflict", message} }

// Unauthorized reports absent or invalid bearer credentials.
func Unauthorized() error { return &Error{401, "unauthorized", "Valid bearer token required"} }

// Backend is the persistence boundary consumed by application use cases.
type Backend interface {
	Authenticate(context.Context, string) (Actor, error)
	Execute(context.Context, Command) (Result, error)
}

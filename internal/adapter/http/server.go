package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	middleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/team-screaner/screener-service/api"
	"github.com/team-screaner/screener-service/internal/adapter/postgres"
	"github.com/team-screaner/screener-service/internal/domain"
	"github.com/team-screaner/screener-service/internal/usecase"
	"github.com/team-screaner/screener-service/web"
)

type requestKey struct{}
type requestContext struct {
	request *http.Request
	body    []byte
	actor   domain.Actor
}
type server struct{ service *usecase.Service }

// New constructs an isolated test server with an ephemeral response key.
func New(db *sql.DB) http.Handler {
	return newServer(db, postgres.New(db))
}

// NewWithResponseKey constructs the service with a persistent deployment key.
func NewWithResponseKey(db *sql.DB, key []byte) http.Handler {
	return newServer(db, postgres.NewWithResponseKey(db, key))
}
func newServer(db *sql.DB, store *postgres.Store) http.Handler {
	svc := usecase.New(store)
	s := &server{service: svc}
	h := api.Handler(api.NewStrictHandlerWithOptions(s, nil, api.StrictHTTPServerOptions{RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
		writeError(w, domain.Invalid("Invalid request"))
	}, ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) { writeError(w, err) }}))
	spec, err := api.GetSpec()
	if err != nil {
		panic(err)
	}
	contract, err := json.Marshal(spec)
	if err != nil {
		panic(err)
	}
	docs := documentationHandler(contract)
	frontend := web.Handler()
	h = middleware.OapiRequestValidatorWithOptions(spec, &middleware.Options{Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}, ErrorHandler: func(w http.ResponseWriter, _ string, _ int) {
		writeError(w, domain.Invalid("Request does not match API contract"))
	}})(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/assets/") {
			frontend.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/openapi.json" || r.URL.Path == "/docs" || strings.HasPrefix(r.URL.Path, "/docs/") {
			docs.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		if r.URL.Path == "/healthz" {
			writeJSON(w, 200, domain.Result{"status": "ok"})
			return
		}
		if r.URL.Path == "/readyz" {
			if err := db.PingContext(ctx); err != nil {
				writeJSON(w, 503, domain.Result{"status": "unavailable"})
				return
			}
			writeJSON(w, 200, domain.Result{"status": "ready"})
			return
		}
		rc := requestContext{request: r}
		public := r.Method == "POST" && (r.URL.Path == "/api/v1/auth/register" || r.URL.Path == "/api/v1/auth/login")
		if !public {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				writeError(w, domain.Unauthorized())
				return
			}
			a, err := svc.Authenticate(ctx, token)
			if err != nil {
				writeError(w, err)
				return
			}
			rc.actor = a
		}
		if r.Body != nil {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 15<<20))
			if err != nil {
				writeError(w, domain.Invalid("Request exceeds 15 MiB"))
				return
			}
			rc.body = body
			r.Body = io.NopCloser(strings.NewReader(string(body)))
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(ctx, requestKey{}, rc)))
	})
}
func (s *server) execute(ctx context.Context, operation, id string) (domain.Result, error) {
	rc, ok := ctx.Value(requestKey{}).(requestContext)
	if !ok {
		return nil, errors.New("missing request context")
	}
	return s.service.Execute(ctx, domain.Command{Operation: operation, ID: id, Method: rc.request.Method, Path: rc.request.URL.Path, Query: rc.request.URL.Query(), Key: rc.request.Header.Get("Idempotency-Key"), Body: rc.body, Actor: rc.actor})
}
func writeError(w http.ResponseWriter, err error) {
	var e *domain.Error
	if errors.As(err, &e) {
		writeJSON(w, e.Status, domain.Result{"code": e.Code, "message": e.Message})
		return
	}
	slog.Error("request failed", "error", err)
	writeJSON(w, 500, domain.Result{"code": "internal_error", "message": "Internal server error"})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("response write failed", "error", err)
	}
}

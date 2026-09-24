//go:build integration

package http_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/team-screaner/screener-service/api"
	"github.com/team-screaner/screener-service/internal/adapter/postgres"
	workbook "github.com/team-screaner/screener-service/internal/adapter/xlsx"
)

func TestHTTPContractAndConcurrency(t *testing.T) {
	server, db := setup(t)
	if seedErr := postgres.Seed(t.Context(), db); seedErr != nil {
		t.Fatal(seedErr)
	}
	token := register(t, server, "contract-owner@example.test")
	other := register(t, server, "contract-other@example.test")
	router := contractRouter(t)
	t.Run("concurrent duplicate mutation returns one audited resource", func(t *testing.T) {
		type result struct {
			response contractResponse
			err      error
		}
		results := make(chan result, 8)
		start := make(chan struct{})
		for range 8 {
			go func() {
				<-start
				response, requestErr := contractRequest(t.Context(), server, "POST", "/organizations", token, "concurrent-organization", map[string]any{"name": "Exactly once"})
				results <- result{response, requestErr}
			}()
		}
		close(start)
		var resourceID string
		for range 8 {
			answer := <-results
			if answer.err != nil {
				t.Fatal(answer.err)
			}
			if answer.response.status != http.StatusOK {
				t.Fatalf("concurrent request returned HTTP %d", answer.response.status)
			}
			body := contractJSON(t, answer.response)
			id := contractString(t, body, "id")
			if resourceID == "" {
				resourceID = id
			}
			if id != resourceID {
				t.Fatal("same idempotency key created different resources")
			}
		}
		organizations := call(t, server, "GET", "/organizations", token, "", nil, 200)
		if len(contractItems(t, organizations)) != 1 {
			t.Fatal("duplicate organization persisted")
		}
		audit := call(t, server, "GET", "/audit?limit=100", token, "", nil, 200)
		mutations := 0
		for _, entry := range contractItems(t, audit) {
			if entry["operation"] == "createOrganization" {
				mutations++
			}
		}
		if mutations != 1 {
			t.Fatalf("audit has %d createOrganization entries, want one", mutations)
		}
		call(t, server, "POST", "/organizations", token, "concurrent-organization", map[string]any{"name": "Changed request"}, 422)
	})
	matrices := call(t, server, "GET", "/matrices", token, "", nil, 200)
	matrixID := contractString(t, contractItems(t, matrices)[0], "id")
	matrix := call(t, server, "GET", "/matrices/"+matrixID, token, "", nil, 200)
	versions := contractArray(t, matrix["versions"])
	versionID := contractString(t, versions[0], "id")
	versionResponse := contractMustRequest(t, server, "GET", "/matrix-versions/"+versionID, token, "", nil, 200)
	version := contractJSON(t, versionResponse)
	levels := contractArray(t, version["levels"])
	assignment := call(t, server, "POST", "/user-matrices", token, "contract-assignment", map[string]any{"matrix_version_id": versionID, "current_level_id": contractString(t, levels[0], "id"), "target_level_id": contractString(t, levels[len(levels)-1], "id")}, 200)
	assignmentID := contractString(t, assignment, "id")
	t.Run("binary export preview and actor isolation", func(t *testing.T) {
		exported := contractMustRequest(t, server, "GET", "/export/xlsx?user_matrix_id="+assignmentID, token, "", nil, 200)
		if exported.header.Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
			t.Fatal("export did not return XLSX media type")
		}
		if !strings.Contains(exported.header.Get("Content-Disposition"), "attachment;") {
			t.Fatal("export is missing attachment disposition")
		}
		sheets, sheetsErr := workbook.Sheets(bytes.NewReader(exported.body))
		if sheetsErr != nil {
			t.Fatal(sheetsErr)
		}
		if len(sheets) != 3 {
			t.Fatalf("personal export has %d sheets, want three", len(sheets))
		}
		preview := call(t, server, "POST", "/import/xlsx", token, "contract-preview", map[string]any{"file_base64": base64.StdEncoding.EncodeToString(exported.body)}, 200)
		if preview["valid"] != true {
			t.Fatal("exported workbook did not produce a valid preview")
		}
		call(t, server, "GET", "/export/xlsx?user_matrix_id="+assignmentID, other, "", nil, 404)
		scoped := call(t, server, "POST", "/tokens", token, "export-scope", map[string]any{"name": "Read matrix only", "scopes": []string{"matrix:read"}, "expires_in_days": 1}, 200)
		call(t, server, "GET", "/export/xlsx?matrix_version_id="+versionID, contractString(t, scoped, "token"), "", nil, 403)
	})
	t.Run("responses satisfy OpenAPI schemas", func(t *testing.T) {
		login := contractMustRequest(t, server, "POST", "/auth/login", "", "", map[string]any{"email": "contract-owner@example.test", "password": "safe-long-password"}, 200)
		contractValidate(t, router, login)
		contractValidate(t, router, versionResponse)
		growth := contractMustRequest(t, server, "GET", "/me/growth-context?user_matrix_id="+assignmentID, token, "", nil, 200)
		contractValidate(t, router, growth)
		facts := contractItems(t, call(t, server, "GET", "/fact-types", token, "", nil, 200))
		skills := contractArray(t, version["skills"])
		evidence := contractMustRequest(t, server, "POST", "/evidence", token, "contract-evidence", map[string]any{"title": "Demonstrated skill", "description": "Concrete reviewable work", "fact_type": contractString(t, facts[0], "key"), "source": "manual", "matches": []any{map[string]any{"skill_key": contractString(t, skills[0], "skill_key"), "confidence": 1, "reason": "Direct demonstration"}}}, 200)
		contractValidate(t, router, evidence)
		evidenceList := contractMustRequest(t, server, "GET", "/evidence", token, "", nil, 200)
		contractValidate(t, router, evidenceList)
		// Prove the validator is enforcing the response shape rather than accepting
		// an unmatched route or skipping response bodies.
		invalid := versionResponse
		invalid.body = []byte(`{"id":7,"status":"published"}`)
		if validationErr := contractValidationError(t.Context(), router, invalid); validationErr == nil {
			t.Fatal("response validator accepted a malformed matrix version")
		}
	})
}

type contractResponse struct {
	request *http.Request
	status  int
	header  http.Header
	body    []byte
}

func contractRequest(ctx context.Context, server *httptest.Server, method, path, token, key string, body any) (result contractResponse, err error) {
	var encoded []byte
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return result, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, server.URL+"/api/v1"+path, bytes.NewReader(encoded))
	if err != nil {
		return result, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, response.Body.Close()) }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return result, err
	}
	return contractResponse{request: request, status: response.StatusCode, header: response.Header.Clone(), body: raw}, nil
}

func contractMustRequest(t *testing.T, server *httptest.Server, method, path, token, key string, body any, status int) contractResponse {
	t.Helper()
	response, requestErr := contractRequest(t.Context(), server, method, path, token, key, body)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	if response.status != status {
		t.Fatalf("%s %s returned HTTP %d, want %d", method, path, response.status, status)
	}
	return response
}

func contractJSON(t *testing.T, response contractResponse) map[string]any {
	t.Helper()
	var value map[string]any
	if decodeErr := json.Unmarshal(response.body, &value); decodeErr != nil {
		t.Fatal("response is not a JSON object")
	}
	return value
}

func contractString(t *testing.T, value map[string]any, key string) string {
	t.Helper()
	text, ok := value[key].(string)
	if !ok || text == "" {
		t.Fatalf("expected nonempty %s string", key)
	}
	return text
}
func contractItems(t *testing.T, value map[string]any) []map[string]any {
	t.Helper()
	return contractArray(t, value["items"])
}
func contractArray(t *testing.T, value any) []map[string]any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatal("expected JSON array")
	}
	items := make([]map[string]any, 0, len(array))
	for _, item := range array {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatal("expected object array element")
		}
		items = append(items, row)
	}
	return items
}

func contractRouter(t *testing.T) routers.Router {
	t.Helper()
	spec, specErr := api.GetSpec()
	if specErr != nil {
		t.Fatal(specErr)
	}
	router, routerErr := legacy.NewRouter(spec)
	if routerErr != nil {
		t.Fatal(routerErr)
	}
	return router
}

func contractValidationError(ctx context.Context, router routers.Router, response contractResponse) error {
	route, params, routeErr := router.FindRoute(response.request)
	if routeErr != nil {
		return fmt.Errorf("find response route: %w", routeErr)
	}
	input := &openapi3filter.ResponseValidationInput{RequestValidationInput: &openapi3filter.RequestValidationInput{Request: response.request, PathParams: params, Route: route}, Status: response.status, Header: response.header, Options: &openapi3filter.Options{IncludeResponseStatus: true}}
	return openapi3filter.ValidateResponse(ctx, input.SetBodyBytes(response.body))
}

func contractValidate(t *testing.T, router routers.Router, response contractResponse) {
	t.Helper()
	validationErr := contractValidationError(t.Context(), router, response)
	if validationErr == nil {
		return
	}
	var schemaErr *openapi3.SchemaError
	if errors.As(validationErr, &schemaErr) {
		t.Errorf("OpenAPI response mismatch for %s %s at %v: %s", response.request.Method, response.request.URL.Path, schemaErr.JSONPointer(), schemaErr.Reason)
		return
	}
	// Validation errors may contain whole payloads. Do not print auth payloads.
	t.Errorf("OpenAPI response validation failed for %s %s (%T)", response.request.Method, response.request.URL.Path, validationErr)
}

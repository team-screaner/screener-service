package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/team-screaner/screener-service/api"
	transport "github.com/team-screaner/screener-service/internal/adapter/http"
)

func TestPublicDocumentation(t *testing.T) {
	handler := transport.New(nil)
	for _, tc := range []struct {
		method, path, content string
		status                int
	}{
		{"GET", "/docs", "", 307},
		{"GET", "/docs/", "Swagger UI", 200},
		{"GET", "/docs/swagger-ui.css", ".swagger-ui", 200},
		{"GET", "/docs/swagger-ui-bundle.js", "SwaggerUIBundle", 200},
		{"GET", "/docs/initializer.js", "validatorUrl: null", 200},
		{"GET", "/docs/missing.js", "", 404},
		{"POST", "/docs/", "", 405},
		{"POST", "/openapi.json", "", 405},
		{"HEAD", "/openapi.json", "", 200},
		{"GET", "/api/v1/matrices", "", 401},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, http.NoBody))
			if res.Code != tc.status || !strings.Contains(res.Body.String(), tc.content) {
				t.Fatalf("status=%d, expected=%d; missing content=%q", res.Code, tc.status, tc.content)
			}
			if tc.method == "HEAD" && res.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
			if tc.path == "/docs" && res.Header().Get("Location") != "/docs/" {
				t.Fatal("documentation redirect points to wrong location")
			}
		})
	}
}

func TestDocumentationServesRuntimeContract(t *testing.T) {
	res := httptest.NewRecorder()
	transport.New(nil).ServeHTTP(res, httptest.NewRequestWithContext(t.Context(), "GET", "/openapi.json", http.NoBody))
	if res.Code != 200 || res.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("contract response: %d %s", res.Code, res.Header().Get("Content-Type"))
	}
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if res.Body.String() != string(expected) {
		t.Fatal("documented contract differs from runtime contract")
	}
}

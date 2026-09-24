# Swagger UI

`GET /docs/` serves Swagger UI; `/docs` redirects there. `GET /openapi.json` serializes the same generated OpenAPI document used by the runtime request validator. Both routes and the static assets are public, read-only GET/HEAD endpoints. Business routes keep their existing authentication and authorization. Documentation endpoints work without database access.

Register or log in, paste the response token into Authorize without the Bearer prefix, and use Try it out. Mutations still require their documented Idempotency-Key. Authorization is kept in page memory only. The UI calls this service on the same origin; external specification validation is disabled.

The browser assets are embedded in the Go binary with `go:embed`, so Docker and native deployments need no CDN or separate static-file installation. Vendored upstream files in `internal/adapter/http/swagger/` are from [Swagger UI v5.33.0](https://github.com/swagger-api/swagger-ui/releases/tag/v5.33.0), under the included Apache 2.0 LICENSE and NOTICE. `index.html` and `initializer.js` are local integration files.

Upstream download prefix: `https://raw.githubusercontent.com/swagger-api/swagger-ui/v5.33.0/`.

| Upstream file | SHA-256 |
|---|---|
| dist/swagger-ui.css | 1ac324f7dcd27e4b9386b4bd6421271ec147e922a22c05ba24b11515e9aa6321 |
| dist/swagger-ui-bundle.js | 62df541529080464a7660adc793eab7128c6193ce3be24ddc1e0e0a4a63edc2f |
| LICENSE | cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30 |
| NOTICE | 0d20d1adef18aee3f40dd258172155521ce702ac445cb5f7b7d60ed32dad2fb2 |

When updating, replace the four upstream files from a pinned release, update these checksums, run the Go tests and lint, then verify rendering, Authorize and Try it out in a browser. No generated API file needs editing.

TDD: the public documentation tests initially returned 401 for the documentation, asset and contract routes. After implementation they verify public serving, runtime contract equality, GET/HEAD behavior, unsupported-method rejection, unknown-asset 404 and unchanged API authentication.

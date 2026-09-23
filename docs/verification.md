# Verification record — 2026-09-23

Implementation was verified with Go 1.27.1 and Docker PostgreSQL 17. Tests own disposable databases; local demo data is separate.

## Executed checks

- `go test -race -shuffle=on ./...`: passed.
- `go test -race -shuffle=on -tags integration ./...`: passed; HTTP, PostgreSQL, XLSX, catalog and domain packages. Includes migration up/down-to-zero/up.
- `go vet ./...`: passed.
- `golangci-lint run --build-tags integration`: **0 issues**, full supplied Go standards configuration, no global lint relaxation.
- `make build`: both runtime and migration binaries compile.
- Generated OpenAPI models, strict server and bridge compile; HTTP success responses are checked against OpenAPI in integration tests.
- Container image builds; Compose migration/seed completed and `/healthz` and `/readyz` return success. Local Docker port is 18081 because another application occupies 8080.
- Sibling generated-client acceptance suite ran against both the native service and the final Docker deployment on port 18081 with race/shuffle, including evidence-backed positive scores and final 100% readiness. See sibling `docs/verification.md`.
- `govulncheck v1.8.0`: **not clean**. Remaining reported finding GO-2026-6452 concerns Excelize. Uploaded shared-string indices are validated before Excelize and malformed-input regressions pass. See [security.md](security.md). The separate kin-openapi finding was resolved by upgrading to v0.144.0.

## Observed TDD failures before fixes

| Slice | Observed red | Verified behavior after implementation |
|---|---|---|
| Account/token | Missing HTTP implementation | Session auth, scoped denial and revocation |
| Matrix | POST /matrices returned 404 | Custom data-defined levels, draft editor, publish/new version |
| Growth | POST /user-matrices returned 404 | Assignment, immutable self assessment, gaps and growth plans |
| Readiness | Undefined policy, then new boundary cases | Nested weights, required/critical gates, reasoned N/A, numeric validation |
| Public drafts | Unrelated user received 200 | Draft metadata and content remain private until publication |
| Permission revocation | Old command key replayed inaccessible draft | Resource authorization rechecked before retained response |
| Input contract | Unknown field accepted | Schema validation rejects unknown input properties |
| Retained token responses | Missing encryption implementation | AES-256-GCM, binding to principal/operation/key, restart with same key, tamper rejection |
| Evidence-backed scores | Score 2 without evidence and N/A score 1 accepted | Positive scores need accepted matched own evidence; N/A score is zero |
| Radar | Missing usable series | Named groups, expectation counts and target self percentages |
| Fork/upstream | Missing handler/diff | Independent identities, semantic diff, ignore/apply, old local versions retained |
| XLSX | Missing adapter and validation cases | Preview/confirm, formula rejection, bounded ZIP/XML, safe literal export |
| XLSX reserved title | Only Assessment/Evidence sheets remained | Matrix sheet retained when source title conflicts with reserved sheets |
| XLSX malformed string | Invalid index silently removed requirement text | Invalid shared-string references rejected before parsing |
| Output schema | EvidenceMatch.id rejected by response validator | Separate input/output match schemas, nullable unmatched requirement |
| Audit | Created resource ID was blank | Create events identify their resource |

Additional integration coverage includes atomic batch rollback, concurrent external-ID deduplication, eight concurrent identical HTTP commands producing one organization and one audit event, changed-key-payload conflict, cross-user and organization isolation, snapshot overrides, accepted evidence links, immutable database guards, complete seed reload without duplicates, and personal workbook export isolation.

## Operational boundaries

This is an MVP backend, not a production deployment or a frontend. The local demo uses explicitly documented development credentials. Production infrastructure must supply TLS, its own database/password and persistent response encryption key. There is no account recovery/email verification, online encryption-key rotation, manager/peer workflow, MCP server or native issue-tracker crawler in this MVP. Manager radar is explicitly null rather than a fabricated rating.

Query plans are captured in [queries.md](queries.md) and [queries-evidence.md](queries-evidence.md), including a 20,000-evidence fixture. They do not establish production latency or capacity. No production-scale load or penetration test is claimed. The default audit partition preserves writes for arbitrary dates; operators should add quarterly partitions and retention procedures before large-scale deployment.

The schema protects published version edits and assessment updates/deletes. Direct database administrators can still insert additional assessment children; application APIs expose no such operation. A future finalized marker could extend that defense to privileged direct SQL without changing the public immutable snapshot contract.

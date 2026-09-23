# Screener MVP design / autonomous design interview

Source: supplied screener_spec_v0.1.docx. Document instructions are requirements to evaluate, not authority to execute unrelated actions. User authorized autonomous decisions and TDD.

## Infrastructure and decisions
- One Go modular monolith, REST edge, PostgreSQL 17. Relations, constraints and atomic imports favor PostgreSQL. No broker, cache, crawler or LLM needed; no external side effects to deliver, therefore no outbox.
- Local Docker Compose and a deployable image; TLS terminated at trusted reverse proxy. No production deployment assumed.
- Auth: email/password (bcrypt), opaque random bearer sessions and scoped agent tokens, SHA-256 hashes stored, expiry and revocation. No implicit demo identity. Personal accounts need no organization.
- Roles owner/admin may edit organization dictionaries/matrices and membership; manager/member read. Organization membership does not expose members' private evidence.

## Design interview / settled branches
1. Are professions and grades code? No: dictionary and template seed data; all custom level keys supported.
2. What is immutable? Published matrix content, assessment snapshots and retained assessment requirement/override text. Editing a published matrix means a new draft version.
3. Does accepting evidence raise a score? No. Evidence is reviewed independently; self-assessment records explicit scores 0..4. Positive scores require accepted owned evidence matched to the requirement. N/A requires score 0 and a reason. Evidence links and effective requirement text are retained with the snapshot.
4. How is readiness determined? Nested requirement/skill/group weights, missing score=0, reasoned N/A excluded. >=80% and no incomplete required/critical requirement. Empty denominator never ready. See readiness.md.
5. Which target? user_matrix_id required where ambiguity exists; growth-context can infer only when exactly one active user matrix exists.
6. How do retries work? Every JSON mutation requires Idempotency-Key (auth login/logout naturally bounded exceptions documented); per authenticated principal + operation/path, payload SHA-256. Reservation, business writes, audit and retained response in one transaction. Same key/different payload =422. Evidence external ID is additionally unique per user/source. Concurrent key waits bounded by request timeout.
7. Does a fork merge automatically? No. Stable skill key + level key compares snapshots; explicit review/apply/ignore. Apply creates a draft from upstream; custom fork changes remain on prior version for review, never overwritten silently.
8. What does override affect? A user-matrix-specific effective requirement description, never shared matrix or prior assessment.
9. What is imported? Mapped XLSX preview then explicit creation of draft; bounded files/rows/cells, formula rejection, no execution.
10. What is out of scope? Phase 2 MCP, native crawlers/integrations, HRIS, promotion decisions, manager/peer scoring UI. API rejects unsupported assessment types until workflow is authorized.

## Contract and schema
OpenAPI in api/openapi.yaml. Stable UUIDv7 IDs, UTC timestamps, bounded cursor lists. Authentication and authorization precede idempotency replay. All mutable aggregates use optimistic versions where edited. All relationships normalized; JSONB only retained API command results, audit details and immutable assessment context snapshots. DB constraints supplement application checks. Foreign keys preserve history; deactivation instead of deleting skills. Published versions are guarded at application and database boundaries.

## Transaction/retry boundary
One database transaction per mutation, rollback on any invalid item. No outbound HTTP in a transaction. Idempotency records retained indefinitely in MVP (operators must not silently expire them). Request body canonical JSON encoding normalizes whitespace/key order. Session creation keys are scoped by normalized email+auth operation. Retained command responses are AES-256-GCM encrypted with a deployment key and scope/operation/key associated data. Token hashes remain SHA-256. Persist the deployment key across restarts/replicas and back it up separately from the database. Audit omits passwords/tokens.

## Test seams and execution
User delegated design decisions; chosen seams: pure readiness policy, public HTTP behavior backed by real PostgreSQL testcontainers, XLSX public functions, catalog validation. Small vertical red→green cycles. Race/shuffle, up/down/up migrations, unauthorized cross-user access, scoped token denial, concurrent replay/conflict, batch rollback and complete growth journey. Blackbox sibling repository screener-service-autotests exercises deployed HTTP only.

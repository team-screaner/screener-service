# Screener personal workspace

The frontend is a same-origin browser application embedded in the existing Go binary. `/` serves the entrypoint, `/assets/` serves local ES modules/styles, and `/api/v1/` retains its existing authorization. Docker has no extra runtime process, public asset CDN or frontend package installation. `web/dist` contains authored, tracked sources, not disposable build output. The Sites-compatible static manifest is retained for a future separate deployment; the current application requires the existing API on the same origin.

## User flows

- Register/login/logout; expired sessions return to login. Session tokens use tab-scoped sessionStorage, never localStorage. Logout also revokes the server session.
- Pick a published matrix from the catalog, inspect requirements and choose current/target levels. Switch between existing assignments.
- Overview: server-calculated readiness, completed requirements, related accepted evidence, growth actions and named competency radar. No invented ratings or progress data.
- Target matrix: search, priorities, latest saved scores, accepted evidence counts and XLSX export.
- Evidence: create manual facts linked to an exact requirement, filter/search, follow safe source links, accept/reject agent suggestions with their optimistic version.
- Self assessment: scores 0–4, required accepted evidence links, reasoned N/A, comments and period. Saving creates an immutable snapshot; accepting evidence never sets scores. Unsaved edits stay in memory across internal navigation and evidence creation. Reload warns about a dirty draft.
- Growth plans: create actions against target requirements with a deadline; change their status while retaining every other plan item.
- XLSX: bounded upload, optional worksheet/header mapping, explicit preview/confirmation, personal draft and explicit publication; export assigned or catalog matrices.
- Agent tokens: explicit scopes and expiry, one-time secret display, copy and revoke.

Organization management, arbitrary matrix editing/fork reconciliation and audit browsing remain available through the API/Swagger rather than this personal-workspace UI. Peer workflows remain deferred. Manager assessment is available through the personal workspace.

## Safety and failure handling

Dynamic text is HTML-escaped; source links allow only HTTP(S). The page uses a same-origin Content Security Policy, local scripts, a restrictive frame policy, and no external fonts or analytics. Drafts are not silently autosaved. Mutations retain their Idempotency-Key after network uncertainty or a server error, allowing a retry without duplicating the action. Successful commands release their key. Session expiry clears the token. Empty, validation, transport and success states are shown in the interface.

List reads follow the server's bounded, request-bound pagination until complete. Large accounts may need incremental UI pagination in a later iteration; no production-scale frontend benchmark is claimed.

## Validation

TDD started with failing public-frontend route tests (401 instead of UI/static assets) and missing JS model/API implementations. Unit tests cover evidence matching, N/A, escaped text, safe URLs, uncertain-command replay, session expiry and paginated filters.

`npm --prefix web run test:live` uses Happy DOM as a DOM test environment with real HTTP requests to `SCREENER_URL`. It drives the actual app module and forms through registration, matrix assignment, evidence creation, a positive assessment, progress refresh, plan status update, agent-token creation/revocation, exported XLSX upload/preview/confirmation/publication and logout. It confirms the revoked session receives 401 and registers a second account in the same page to verify that the first account’s draft period and private state do not carry over. That isolation check first failed with the previous period visible, then passed after central session-state cleanup. It creates isolated synthetic accounts in the chosen development deployment and does not delete unrelated data. It is not a browser rendering or screenshot test.

Run `npm ci --prefix web`, `make test-web`, `make test`, `make test-integration`, and `SCREENER_URL=http://127.0.0.1:18081 make test-web-live`. Node dependencies are test-only. The CI workflow executes these checks against Docker Compose as well as the existing generated-contract, lint and binary-build checks.

Browser transport regression (2026-09-24): assigning native `fetch` directly to the API client caused its calls to use the client instance as the receiver. Browsers rejected this with an illegal invocation, which the UI reported as a connection failure; the Node/Happy DOM transport did not expose that difference. The default transport is now bound to `globalThis`. A receiver-sensitive test failed with the reported connection message before the fix and passes afterward. Login was also verified in the actual in-app browser against the rebuilt local service, reaching the authenticated workspace.


## Manager comparison

The overview radar overlays self assessment (blue) and the designated manager’s assessment (orange, dashed outline) on the same 0–100 scale and the same ordered competency axes. Five groups produce a pentagon; other group counts retain their actual axes, while one or two groups use paired bars. Translucent fills show overlap. A named comparison table includes both values and manager-minus-self differences in percentage points. Missing manager assessment stays visibly absent, never a fabricated zero. The period and manager author identify the pair of snapshots; saving a new self assessment hides the previous manager contour until that new snapshot is reviewed.

An employee designates an already registered manager by email in the overview, explicitly sharing this trajectory and its linked evidence. No email is sent. Access can be replaced or revoked. The manager opens **Оценки команды** without needing their own matrix and evaluates the employee’s latest self snapshot. Every target requirement is included. The form shows the saved requirement wording, self score/comment and accepted evidence details; manager scores remain independently entered, with accepted evidence required above zero and a reason for N/A. Saving creates an immutable manager snapshot tied to that self assessment and period. A fresh self assessment during review produces a server conflict; **Обновить снимок** reloads the current baseline, with confirmation before discarding a dirty review. Reviewer drafts remain separate from self drafts and are cleared at logout/session expiry. A replacement reviewer starts an independent draft; another manager’s previous snapshot is attributed explicitly and never prefilled into their scores.

Radar DOM tests started red and cover two five-point polygons, missing-manager absence, paired bars, accessible comparison and escaped names. The live UI journey additionally creates an isolated manager, designates them, signs into the manager account, saves a differing assessment, returns to the owner and checks both series (polygons or paired bars according to the template group count), creates a new self snapshot and checks that the old manager series disappears, then revokes access. Existing personal-workspace and session-isolation checks remain in the same journey.

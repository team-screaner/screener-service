# Manager review and competency comparison

The employee delegates review of one assigned matrix to an existing registered account. This is explicit per-matrix sharing, independent of organization roles; it does not grant organization administration or let a reviewer edit the employee's facts. No invitation email is sent. Peer/360 reviews and organization-wide reporting remain outside this slice.

## Contract and workflow

1. Owner uses `GET/PUT/DELETE /api/v1/user-matrices/{id}/reviewer`. PUT takes an email; owner cannot appoint themselves. Mutations require an idempotency key.
2. Designated manager sees a cursor-paginated `GET /api/v1/reviews` inbox and `GET /api/v1/reviews/{id}` with matrix context, latest self assessment, and any saved manager assessment of that exact snapshot. Shared evidence is limited to accepted facts linked to exact requirements in that matrix.
3. Manager posts `/api/v1/assessments` with `type: manager`, `reviewed_assessment_id`, matching period, and all active target requirements. Scores remain 0–4; positive scores require accepted employee evidence for the exact requirement. N/A requires zero and a reason. Missing/foreign/duplicate requirements are rejected. A stale self snapshot returns 409 so the manager can reload explicitly.
4. Employee's `/api/v1/me/radar` keeps self readiness and adds an aligned manager series, manager author, assessment ID and period. Both series use the same matrix version, group order, 0–100 scale and weighted policy. A new self assessment clears the displayed manager comparison until reviewed. Previously saved assessments remain immutable and readable to their owner.

The UI provides manager assignment/revocation in Overview and a separate Team Assessments section, including for managers without a personal matrix. Blue self and orange manager polygons have translucent fills; the manager border is dashed. Named table rows show both percentages and manager-minus-self differences in percentage points. Matrices with fewer than three groups use paired bars. A five-group matrix produces a pentagon; other group counts retain their actual axes. An absent manager assessment is explicitly absent, never fabricated as a second zero contour. N/A follows the existing weighted policy independently for each assessment.

Saved historical manager ratings remain visible after access revocation or replacement until a new self snapshot is created or another manager review is saved. Author attribution identifies who supplied that rating. A newly assigned manager cannot inherit the previous review as their own draft.

## Storage and consistency

Migration `00004_manager_reviews.sql` adds a normalized `matrix_reviewers` relation keyed by assignment, indexed by reviewer/assignment; immutable assessments gain author and reviewed-snapshot foreign keys plus a manager attribution check and a partial reviewed-snapshot index. Existing self assessments remain valid without backfilling or modifying immutable records. Down migration removes the new metadata and grants, so rollback loses manager attribution; back up before downgrade with real review data.

Grant changes and assessment writes lock the same assignment row. Mutable reviewer authorization is rechecked before idempotency replay. A revoke that commits first prevents both a new write and replay of a previously successful write; a write already holding the lock completes before the revoke. Reads use the existing repeatable-read transaction boundary. Responses and audit rows commit atomically with snapshots. Agent tokens cannot act as managers or access the review inbox. This flow needs no broker, cache or external messaging.

## Validation

`TestManagerReviewAndRadar` first failed because the reviewer operation was unavailable. It now verifies the complete HTTP/PostgreSQL journey, owner/reviewer/stranger isolation, rejected self-review, evidence requirements, complete target coverage, period matching, response contracts (including nullable data), identical axes with self=100% and manager=50%, idempotent replay, agent denial, unrelated evidence exclusion, revocation, and stale/new snapshot behavior. Migration Up/Down/Up is exercised by the integration fixture.

Frontend tests cover actual five-axis polygon geometry, axis alignment, missing-manager rendering, paired bars, author-specific drafts, and the live owner → manager → owner flow against PostgreSQL. A separate synthetic five-group browser fixture was used to sign in as manager, submit five evidence-backed scores, and inspect the employee's overlaid chart and comparison table in the actual browser. These records are marked as demonstrations and do not represent real employee evaluations.

Final local checks: `make test`, `make test-integration` (race/shuffle), 13 frontend tests, live forms against the rebuilt Docker service, and standards lint (0 issues) passed. Independent generated-client acceptance coverage is updated in `screener-service-autotests`. Eleven new review-access/list/write SQL statements were inspected with `EXPLAIN (ANALYZE, BUFFERS)` on synthetic development records inside rollback transactions; these checks are not a production-scale performance benchmark.

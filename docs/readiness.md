# Readiness policy

`domain.CalculateReadiness([]domain.RequirementProgress)` returns a
`domain.ReadinessResult` and an error. It is a pure calculation and never changes
the supplied slice. Missing assessments must be included with `Score: 0`; the
caller supplies the complete requirement universe, including unanswered items.

Every applicable requirement has a score from 0 to 4. A skill's percentage is the
weighted mean of its requirements' scores multiplied by 25. A group's percentage
is the weighted mean of its skills' percentages. Overall readiness is the weighted
mean of the groups' percentages. A skill or group weight applies once at its own
level, regardless of how many child requirements it contains.

The decision is ready only when the overall percentage is at least 80 and every
applicable required or critical requirement has score 4. This is an explicit MVP
policy choice; reaching 80 alone does not override a mandatory gap. Empty and
entirely N/A inputs produce zero percent and are not ready.

N/A requires a nonblank reason and excludes the requirement from denominators,
gaps, and blockers. A skill or group with no applicable children is omitted.
Any applicable requirement below 4 appears in `Gaps`, sorted by requirement ID.
Critical gaps additionally appear as sorted IDs in `CriticalGaps`. `Groups` is
sorted by group ID. Empty collections use nil slices in the domain; transport
adapters decide the API's JSON representation.

Validation rejects nonfinite or out-of-range scores, nonpositive or nonfinite
weights, blank IDs, duplicate requirement IDs, a skill assigned to two groups,
inconsistent weights for the same skill or group, and unjustified N/A. Validation
also applies to excluded requirements. All failures wrap `ErrInvalidReadiness`
and return a zero result. No partial result should be consumed on error. Weights
are normalized before averaging to prevent overflow for valid finite weights.

Example with independent expected values: skill s1 has scores 4 and 0 weighted
1:3, giving 25%; skill s2 is 100%. In g1 their skill weights are 1:3, giving
81.25%. Group g2 is 50%; with group weights g1:g2 = 1:3, overall readiness is
57.8125%.

## TDD evidence

All cycles used `go test -race -shuffle=on ./internal/domain` on 2026-09-23.
Tests were written and executed before each implementation increment:

| Increment | Observed red | Observed green |
| --- | --- | --- |
| Empty assessment | `undefined: CalculateReadiness` | Empty result returns zero and not ready |
| Hierarchical weights and decision gates | Nested total was 0 instead of 57.8125; complete and threshold cases were false | Nested weights, inclusive 80%, required/critical blockers, missing score pass |
| N/A and breakdown | N/A scenario returned 10% instead of 100%; expected gaps/groups absent | Exclusion, empty effective scope, sorted gaps/groups and input immutability pass |
| Validation and numeric bounds | Invalid values and ambiguous hierarchy accepted; extreme finite weights returned NaN instead of 50 | All validation cases and overflow regression pass |

Unit tests are table driven where scenarios share setup and use parallel isolated
cases. They require no infrastructure. Domain code imports only Go's standard
library. Parent service checks run separately; no DB integration is needed for
this pure calculation.

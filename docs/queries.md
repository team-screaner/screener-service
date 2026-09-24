# Executed catalog query plans

`TestCatalogReadQueryPlans` in `internal/adapter/postgres/catalog_integration_test.go` executes `EXPLAIN (ANALYZE, BUFFERS)` for the four catalog listing SQL statements, including JSON projection, evidence signals, caller visibility, ordering and first-page predicates.

Run:

```sh
go test -race -shuffle=on -tags integration ./internal/adapter/postgres -run TestCatalogReadQueryPlans -count=1 -v
```

Measured on 2026-09-23 using `postgres:17-alpine` in local Docker. Fixture: 236 system skills, 236 evidence signals, 24 fact types, one user, one organization and its owner membership. Statistics were refreshed with `ANALYZE`. Page limit was 51, including the look-ahead row. These measurements describe this small fixture, not a production throughput or latency guarantee.

| Query | Result rows | Execution time | Shared buffer hits | Main plan |
|---|---:|---:|---:|---|
| Visible skills, including signals | 51 | 0.907 ms | 251 | Primary-key index scan; per-row signal aggregate |
| User organizations | 1 | 0.021 ms | 2 | Nested loop over small sequential scans; sort |
| Organization members | 1 | 0.020 ms | 2 | Nested loop over small sequential scans; sort |
| Fact types | 24 | 0.035 ms | 1 | Sequential scan; sort |

Selected verbatim plan lines from the successful run:

```text
visible skills Limit  (cost=0.14..416.46 rows=51 width=48) (actual time=0.045..0.880 rows=51 loops=1)
  -> Index Scan using skills_pkey on skills s  (cost=0.14..1926.61 rows=236 width=48) (actual time=0.044..0.875 rows=51 loops=1)
     SubPlan 1
       -> Aggregate  (cost=6.97..6.98 rows=1 width=32) (actual time=0.012..0.012 rows=1 loops=51)
          -> Seq Scan on evidence_signals  (cost=0.00..6.95 rows=1 width=67) (actual time=0.005..0.009 rows=1 loops=51)
             Rows Removed by Filter: 235
organizations -> Nested Loop  (cost=0.00..2.04 rows=1 width=48) (actual time=0.009..0.010 rows=1 loops=1)
members -> Nested Loop  (cost=0.00..2.04 rows=1 width=48) (actual time=0.008..0.009 rows=1 loops=1)
fact types -> Sort  (cost=1.91..1.97 rows=24 width=48) (actual time=0.027..0.028 rows=24 loops=1)
```

The skill page uses the primary key to preserve cursor order. PostgreSQL chose repeated sequential scans of the very small signals table; the existing `(skill_id, signal)` primary key is available when cardinalities make index access cheaper. Organization and member sequential scans are expected for this one-row fixture. No planner settings were forced.

Still required before production sizing: repeat plans with representative organization membership, personal/organization skill counts, filter selectivity, long signal lists and later cursor pages; include concurrent writes and read load. This document does not claim that every service query, mutation or import/export has been profiled.

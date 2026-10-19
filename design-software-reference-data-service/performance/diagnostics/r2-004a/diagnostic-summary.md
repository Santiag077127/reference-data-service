# R2-004A Diagnostic Summary

## Current state

- Branch: `hu-003-dev`
- Database: `ds-reference-clean-postgres-1`, `localhost:15436`, `design-software-develop`
- R2 dataset remains loaded.
- `ix_catalog_detail_active_catalog_display_order_code` exists.
- No production SQL, index, Liquibase file, or data was changed.

## Query B finding

The current query uses a CTE that selects the first 20 active catalogs, then a `LEFT JOIN` over all active `catalog_detail` rows. PostgreSQL chooses a `Seq Scan` plus `Hash Right Join` because approximately 90% of `catalog_detail` is active. The partial index is not selective enough for that global scan shape, and the planner estimates the hash join as cheaper.

Normal plan: 24.806 ms, `shared hit=1470`, `Seq Scan` + `Hash Right Join` + `Sort`.

## Forced-index finding

With `SET LOCAL enable_seqscan = off` inside a transaction, PostgreSQL used the R2-003 index through a `Nested Loop Left Join` and produced 3.821 ms, but with `shared hit=1864 read=12`. This is a diagnostic comparison only, not a persisted configuration or production solution.

## Alternative query finding

A semantically equivalent `LEFT JOIN LATERAL` shape caused PostgreSQL to use the R2-003 index for each selected catalog and returned 4.123 ms with `shared hit=1873`. It preserves the first-20 catalog pagination, active filters, LEFT JOIN behavior, and ordering. The plan still has an incremental sort, but it avoids scanning all 90,000 active details.

## Global regression finding

The historical k6 AFTER run was slower for A, B, and C, including Query A which does not use R2-003. The current point-in-time inspection found no locks, active long-running queries, connection saturation, or PostgreSQL resource pressure. The service has no explicit database pool limits in source. This is insufficient to prove the cause of the historical global regression.

## Recommended next action

`B. SQL QUERY OPTIMIZATION`

The strongest evidence-supported next step is to evaluate the LATERAL query shape for Query B, without adding another index yet. The existing index is effective for Query C but cannot improve the current Query B hash-join shape. This recommendation is not implemented in this phase.

Before implementation, repeat controlled measurements with the same environment and verify API semantic equivalence. Do not change thresholds, dataset, or production code during this diagnostic.

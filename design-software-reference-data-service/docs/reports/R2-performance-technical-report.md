# R2 Performance and Data — Technical Report

## 1. Objective

R2 evaluated performance and data access in the Reference Data domain. The
acceptance criteria were: k6 against the main endpoints, 50 concurrent VUs for
five minutes, global p95 below 300 ms, error rate below 1%, before/after
`EXPLAIN (ANALYZE, BUFFERS)` for three relevant queries, database changes via
Liquibase with rollback verification, and before/after evidence.

## 2. Scope

The measured paths were:

- **Query A:** municipalities by department.
- **Query B:** active catalog report.
- **Query C:** catalog details.

The workload used real HTTP endpoints and real IAM authentication; endpoint
responses were not mocked. Workload and thresholds are defined in
[`performance/k6/baseline.js`](../../performance/k6/baseline.js).

## 3. Test conditions

The official BEFORE and final AFTER runs used the same 50-VU, five-minute k6
workload against the isolated R2 environment and deterministic R2 performance
dataset. Acceptance thresholds were global p95 `< 300 ms` and HTTP error rate
`< 1%`. Environment credentials and connection values are intentionally not
recorded in this report.

## 4. BEFORE baseline

| Metric | Official BEFORE |
|---|---:|
| HTTP requests | 107,807 |
| Iterations | 107,806 |
| Throughput | 358.914859 req/s |
| Global p95 | 329.15 ms |
| Error rate | 0.00% |
| Query A p95 | 247.49 ms |
| Query B p95 | 376.57 ms |
| Query C p95 | 323.84 ms |

**Result:** the baseline failed the p95 criterion. The aggregate console
evidence is summarized in
[`performance/evidence/r2-004-final.md`](../../performance/evidence/r2-004-final.md).

## 5. SQL analysis with EXPLAIN (ANALYZE, BUFFERS)

### Query A

Query A already had suitable municipality access paths. Its plan remained
essentially unchanged, so no additional index was justified. Its final HTTP
improvement is not attributed to the `catalog_detail` index. See
[`performance/explain/after/comparison.txt`](../../performance/explain/after/comparison.txt).

### Query B

The prior plan made broad active `catalog_detail` access through `Seq Scan`,
`Hash Right Join` and `Sort`. Its isolated diagnostic execution was about
24.806 ms. The query was reshaped as a semantically equivalent `LEFT JOIN
LATERAL`; the optimized isolated execution was about 4.417 ms, approximately
82.19% lower.

Semantic equivalence was verified as:

```text
old_minus_new = 0
new_minus_old = 0
ordered differences = 0
```

The optimized plan uses selective per-catalog, index-assisted access. See
[`performance/explain/after-query-b-optimization/comparison.txt`](../../performance/explain/after-query-b-optimization/comparison.txt).

### Query C

The AFTER plan uses the partial index: the count uses `Index Only Scan`, the
page uses `Index Scan`, and the explicit sort seen before is removed. Captured
buffer access was substantially lower for this path. See
[`performance/explain/before/query-c.txt`](../../performance/explain/before/query-c.txt)
and [`performance/explain/after/query-c.txt`](../../performance/explain/after/query-c.txt).

## 6. Database change

The database change is the partial index:

```sql
ix_catalog_detail_active_catalog_display_order_code
ON parameterization.catalog_detail(catalog_id, display_order, code)
WHERE is_active = true
```

It was selected because Query C filters active details by catalog and orders by
the indexed detail attributes; the same selective access supports the reshaped
Query B. The local service evidence confirms index presence and use in the
captured plans: [`performance/explain/after/indexes.txt`](../../performance/explain/after/indexes.txt).

The Liquibase changeset belongs to the separate `reference-data-db` repository.
Verified external-repository evidence is: apply **PASS**, `rollback-count 1`
**PASS**, and reapply **PASS**. Raw Liquibase artifacts are not present in this
repository, so no service-local path is asserted.

## 7. Sustained-load diagnostics

Intermediate sustained runs exposed intermittent HTTP 503 responses. The
diagnostics localized affected failures to the PostgreSQL repository connection
layer in those runs. See
[`performance/diagnostics/r2-004c/`](../../performance/diagnostics/r2-004c/).

This did **not** prove PostgreSQL `max_connections` exhaustion, did **not**
prove Query B was the sole cause, and showed runtime/environment variability.
It motivated bounded `database/sql` pool management and safe pool telemetry.

## 8. Connection pool decision

| Setting | Value |
|---|---:|
| MaxOpenConns | 40 |
| MaxIdleConns | 10 |
| ConnMaxLifetime | 30 minutes |
| ConnMaxIdleTime | 5 minutes |

The decision bounds physical connection expansion, retains moderate reusable
idle capacity, and uses a lifetime longer than the five-minute acceptance
window. Safe numeric `DB.Stats()` telemetry supports subsequent observation.
When all 40 connections are busy, controlled `database/sql` waiting can occur.
This configuration is not claimed as the sole source of the final result.

## 9. FINAL AFTER acceptance run

| Metric | Final AFTER |
|---|---:|
| VUs / duration | 50 / 5 min |
| HTTP requests | 232,334 |
| Iterations / interrupted | 232,333 / 0 |
| Throughput | 773.76104 req/s |
| Global p95 | 102.55 ms |
| Error rate | 0.00% |
| Query A p95 / errors | 70.97 ms / 0.00% |
| Query B p95 / errors | 95.68 ms / 0.00% |
| Query C p95 / errors | 115.41 ms / 0.00% |

**Result:** p95 threshold **PASS**; error threshold **PASS**. The authoritative
console output exists in the ignored results directory and is referenced by
the final evidence document without copying sensitive runtime data.

## 10. BEFORE vs AFTER

| Metric | BEFORE | FINAL AFTER | Change |
|---|---:|---:|---:|
| Global p95 | 329.15 ms | 102.55 ms | approximately 68.8% lower |
| Error rate | 0.00% | 0.00% | unchanged |
| Throughput | 358.914859 req/s | 773.76104 req/s | approximately 115.6% higher |
| Query A p95 | 247.49 ms | 70.97 ms | lower |
| Query B p95 | 376.57 ms | 95.68 ms | lower |
| Query C p95 | 323.84 ms | 115.41 ms | lower |

The end-to-end delta is evidence for the complete accepted configuration, not
proof that any one individual change accounts for all improvement.

## 11. Verification

- `go test ./...`: **PASS**.
- `git diff --check`: **PASS**.
- Query B semantic equivalence: **PASS**.
- Liquibase apply / rollback / reapply: **PASS** in external
  `reference-data-db` evidence.

## 12. Limitations and attribution

Results are from the isolated R2 test environment. Runtime variability was
observed during diagnostics. The final result represents the complete accepted
configuration; it does not independently prove that the pool, partial index or
Query B change alone produced the complete delta. PostgreSQL
`max_connections` exhaustion was not established.

## 13. Conclusion

**R2 LOAD-TEST ACCEPTANCE: PASS**

For 50 VUs during five minutes:

- p95 = 102.55 ms < 300 ms.
- Errors = 0.00% < 1%.

R2 is supported by k6 BEFORE/AFTER evidence, three-query EXPLAIN evidence,
the Query B SQL optimization, the partial Liquibase index with externally
verified rollback validation, bounded database-pool management, automated
tests, and [ADR-001](../adr/ADR-001-r2-catalog-performance-and-pool-management.md).

## 14. Defense notes

1. The official baseline failed p95, not errors: 329.15 ms versus the 300 ms target.
2. Query A already had suitable index access, so adding another index was not justified.
3. The partial index matches active catalog-detail filtering and ordering used by Query C.
4. Query B required `LEFT JOIN LATERAL` because the original broad join did not use selective index access.
5. `enable_seqscan=off` was diagnostic-only and rejected as a production planner override.
6. The pool was bounded to control physical connection growth and expose controlled waits, not because exhaustion was proven.
7. Historical 503 diagnostics showed shared connection-layer variability across A, B and C; they do not identify one sole cause.
8. The accepted final configuration reached p95 102.55 ms, 0.00% errors and 773.76104 req/s.

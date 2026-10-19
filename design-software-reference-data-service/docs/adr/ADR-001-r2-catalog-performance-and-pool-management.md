# ADR-001: R2 catalog performance and bounded database pool management

- Status: Accepted
- Date: 2026-10-19
- Scope: `reference-data-service` R2 Performance and Data challenge

## Context

R2 requires the protected Reference Data workload to sustain 50 virtual users
for five minutes with global p95 below 300 ms and HTTP failures below 1%.

The original official baseline recorded global p95 of 329.15 ms, 0.00% errors
and throughput of 358.914859 requests/s. Query B and Query C were selected for
SQL analysis. Query B originally joined the selected catalogs to the broad set
of active catalog details, while Query C filtered and sorted catalog details.

Historical sustained diagnostics also recorded intermittent HTTP 503 bursts
across Query A, B and C. The associated repository failures were at the
PostgreSQL connection layer, but the historical evidence does not prove
PostgreSQL `max_connections` exhaustion, does not isolate Query B as the
cause, and shows runtime/environment variability.

Detailed final evidence is consolidated in
[R2-004 final evidence](../../performance/evidence/r2-004-final.md).

## Decision

Adopt the following combined performance decision for R2.

1. Use the selective partial PostgreSQL index
   `ix_catalog_detail_active_catalog_display_order_code` on
   `parameterization.catalog_detail(catalog_id, display_order, code)` with
   `WHERE is_active = true`.
2. Reshape Query B (`ReportActiveCatalog`) from the broad active-detail join to
   a semantically equivalent `LEFT JOIN LATERAL`, allowing selective detail
   access per paged catalog while retaining active filters, fields, pagination,
   `LEFT JOIN` behavior and ordering.
3. Configure the single shared `database/sql` pool explicitly:

   | Setting | Value |
   |---|---:|
   | `MaxOpenConns` | 40 |
   | `MaxIdleConns` | 10 |
   | `ConnMaxLifetime` | 30 minutes |
   | `ConnMaxIdleTime` | 5 minutes |

The pool configuration is implemented in
[`cmd/reference-data-api/main.go`](../../cmd/reference-data-api/main.go).
Safe numeric `DB.Stats()` logging remains enabled through
[`internal/adapter/out/postgres/pool_stats.go`](../../internal/adapter/out/postgres/pool_stats.go).

## Alternatives considered

### Keep the original Query B

Rejected. Its original plan used a broad `Seq Scan` of active
`catalog_detail`, `Hash Right Join` and `Sort`, with isolated execution around
24.806 ms in the diagnostic measurement.

### Add only the partial index

Rejected as sufficient for Query B. The original broad join shape did not use
the index in captured plans. The index is independently beneficial to Query C,
but it did not make the original Query B plan selective.

### Force planner settings, such as `enable_seqscan=off`

Rejected for production. It was useful only as a diagnostic experiment to
compare candidate plans. Persisting a planner override would affect unrelated
queries and replace cost-based planning with a global operational assumption.

### Use `LEFT JOIN LATERAL` for Query B

Accepted. It preserves observed result semantics and lets PostgreSQL access
active details per paged catalog using the partial index.

### Leave `database/sql` defaults

Rejected. The default `MaxOpenConns=0` allows unbounded physical connection
growth and the default idle pool retains only two connections. Sustained-run
diagnostics showed intermittent connection-layer failures and later pool
statistics showed activity near the prior unconstrained connection count.
Those observations justify bounded-pool management, but do not prove that the
default pool alone caused the prior failures.

### Explicitly bound the shared pool

Accepted. The cap of 40 reserves capacity below PostgreSQL's observed limit of
100, while 10 idle connections provide a limited reusable set. The 30-minute
lifetime exceeds the five-minute acceptance run; the five-minute idle limit
allows inactive connections to be released after demand subsides.

## Evidence

### Query analysis

- Query A already used its appropriate municipality indexes and did not justify
  an additional index. See
  [`performance/explain/after/comparison.txt`](../../performance/explain/after/comparison.txt).
- Query B semantic equivalence was measured as `old_minus_new = 0`,
  `new_minus_old = 0`, and ordered differences `= 0`. Its optimized isolated
  execution was approximately 4.417 ms versus approximately 24.806 ms before,
  a reduction of approximately 82.19%. The optimized plan uses index-assisted
  access, `Nested Loop Left Join` and `Incremental Sort`. See
  [`performance/explain/after-query-b-optimization/comparison.txt`](../../performance/explain/after-query-b-optimization/comparison.txt).
- Query C uses the partial index for the count and page plans; the captured
  page plan removes the explicit sort. See
  [`performance/explain/after/query-c.txt`](../../performance/explain/after/query-c.txt).
- The service-local index record is in
  [`performance/explain/after/indexes.txt`](../../performance/explain/after/indexes.txt).

The Liquibase changeset itself is owned by `reference-data-db`, not this
repository. Its apply, `rollback-count 1`, and reapply were tested there. This
ADR does not invent a local reference-data-db path or modify that repository.

### Final load-test acceptance evidence

The authoritative final console output exists at
[`performance/results/r2-after-pool-final-5m.txt`](../../performance/results/r2-after-pool-final-5m.txt).
The results directory is ignored, so the tracked final evidence stores only
safe aggregate values.

| Metric | Original official baseline | Final acceptance run |
|---|---:|---:|
| Workload | 50 VUs / 5 min | 50 VUs / 5 min |
| Global p95 | 329.15 ms | 102.55 ms |
| Error rate | 0.00% | 0.00% |
| Throughput | 358.914859 req/s | 773.76104 req/s |
| Query A p95 | 247.49 ms | 70.97 ms |
| Query B p95 | 376.57 ms | 95.68 ms |
| Query C p95 | 323.84 ms | 115.41 ms |

The final run completed 232,334 HTTP requests and met both acceptance
thresholds: p95 below 300 ms and error rate below 1%. Compared with the
baseline, global p95 was approximately 68.8% lower and throughput was
approximately 115.6% higher.

## Consequences

### Positive

- R2 load-test acceptance thresholds are satisfied.
- Query B accesses catalog details selectively for each paged catalog.
- Query C has an index-supported count/page path without the captured explicit
  page sort.
- Database connection concurrency is explicitly bounded and observable through
  safe numeric pool statistics.

### Trade-offs and limitations

- A bounded pool can introduce `database/sql` waiting under high concurrency;
  this is expected to be observed through `WaitCount` and `WaitDuration`.
- The partial index consumes storage and has write-maintenance cost.
- The LATERAL query is more specialized than the former broad join.
- End-to-end results depend on runtime conditions. The accepted result must not
  be interpreted as proof that one individual change produced the entire
  improvement.

## Rollback

The components are independently reversible:

- Query B can return to its former join shape through a service change.
- The pool settings can be retuned or reverted through service configuration
  code.
- The partial index rollback is owned by its Liquibase changeset in
  `reference-data-db`; its `apply -> rollback-count 1 -> reapply` path was
  already tested in that repository.

## Security and observability

Repository diagnostics log only safe classifications and Go error types.
Pool diagnostics log only aggregate numeric `DB.Stats()` fields. They must not
log DSNs, addresses, credentials, JWTs, authorization headers, SQL or SQL
parameters, request/response bodies, or PII.

## Result

The combined SQL/index and bounded-pool decision is accepted. The final R2
acceptance run is **PASS**. Historical diagnostics remain evidence of
intermittent connection-layer failures and runtime variability; they are not a
confirmed single-cause explanation for the final improvement.

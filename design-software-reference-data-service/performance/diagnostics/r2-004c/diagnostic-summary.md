# R2-004C — HTTP error diagnostic

The official 50 VU / 5 minute run meets the latency criterion (global p95
197.03 ms) but fails the error-rate criterion (26.75%, or 41,038 of 153,411
requests). Query A, B, and C have comparable non-200 rates, so the evidence
does not attribute the failures to the Query B optimization.

The current classification remains **J. UNKNOWN — MORE EVIDENCE REQUIRED**.
Two independent sustained diagnostics reproduced HTTP 503 bursts (24.39% and
25.05% error rates), while the 50 VU / 30 second diagnostic was clean. Query
A, B, and C fail together in bursts at 90-120 and 210-240 seconds, so Query B
is not the responsible component. The source limits a protected read's 503
origin to JWKS-validation unavailability or a repository error mapped by the
handler. The five-minute JWKS cache TTL does not align with the two-minute
burst period, and token expiry would return 401. However, the application
does not log the underlying repository error; consequently, it is not sound
to declare a database pool, PostgreSQL, or IAM root cause yet.

See `sustained-run-observations.txt` for the safe run data. The included
sustained diagnostic captures counters by response status, endpoint, and
30-second test window without writing secrets or response bodies.

Run from the PowerShell session that already has the K6_* environment values:

```powershell
k6 run --vus 50 --duration 5m performance/diagnostics/r2-004c/sustained-status-diagnostic.js
```

This is a controlled diagnostic, not acceptance evidence. The first non-200
metric name identifies its endpoint and 30-second window; update the safe
status-code summary with the console counters before selecting a corrective
action.

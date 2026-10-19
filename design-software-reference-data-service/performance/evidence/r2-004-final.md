# R2-004 — Evidencia final de rendimiento y datos

## Objetivo y criterio de aceptación

Validar los tres flujos protegidos de Reference Data con la carga oficial de
R2: 50 VUs durante cinco minutos, con `p95 < 300 ms` y
`http_req_failed < 1 %`.

La carga y los thresholds están definidos sin cambios en
[`../k6/baseline.js`](../k6/baseline.js). La evidencia se tomó contra el
entorno local aislado de R2 con el dataset representativo ya cargado.

## Hechos medidos: k6 BEFORE y AFTER final

La salida de consola final existe en
[`../results/r2-after-pool-final-5m.txt`](../results/r2-after-pool-final-5m.txt).
Ese directorio está excluido de Git, por lo que este documento conserva solo
métricas agregadas seguras. El BEFORE oficial está en
[`../results/baseline-before-console.txt`](../results/baseline-before-console.txt).

| Métrica | BEFORE oficial | AFTER final | Delta |
|---|---:|---:|---:|
| VUs / duración | 50 / 5 min | 50 / 5 min | Mismo workload |
| Solicitudes HTTP | 107,807 | 232,334 | +115.5 % |
| Iteraciones completas / interrumpidas | 107,806 / 0 | 232,333 / 0 | — |
| Throughput | 358.914859 req/s | 773.76104 req/s | +115.6 % |
| p95 global | 329.15 ms | 102.55 ms | -68.8 % |
| Error rate | 0.00 % | 0.00 % | Sin cambio |
| Query A p95 / errores | 247.49 ms / 0.00 % | 70.97 ms / 0.00 % | — |
| Query B p95 / errores | 376.57 ms / 0.00 % | 95.68 ms / 0.00 % | — |
| Query C p95 / errores | 323.84 ms / 0.00 % | 115.41 ms / 0.00 % | — |

| Threshold | Resultado AFTER final | Estado |
|---|---:|---|
| `http_req_duration` p95 < 300 ms | 102.55 ms | PASS |
| `http_req_failed` < 1 % | 0.00 % | PASS |

## Evidencia SQL: A, B y C

Los planes BEFORE están en [`../explain/before/`](../explain/before/) y los
planes AFTER iniciales en [`../explain/after/`](../explain/after/). La
optimización específica de Query B y su comparación semántica están en
[`../explain/after-query-b-optimization/`](../explain/after-query-b-optimization/).

### Query A — municipios

**Hecho medido.** Query A ya usaba los índices existentes
`ix_municipality_department_id` y `uq_municipality_department_id_name`; su
forma de plan se mantiene esencialmente igual antes y después. Ver
[`../explain/after/comparison.txt`](../explain/after/comparison.txt).

**Conclusión.** No se justificó un índice nuevo para Query A. La mejora de su
latencia HTTP final no se atribuye al índice de `catalog_detail`.

### Query B — reporte de catálogos activos

**Hecho medido.** La forma original hacía un `LEFT JOIN` amplio: `Seq Scan`
de detalles activos, `Hash Right Join` y `Sort`. El plan BEFORE aislado fue
23.066 ms; el diagnóstico de la forma de producción previa a la reescritura
midió aproximadamente 24.806 ms. Ver
[`../explain/before/query-b.txt`](../explain/before/query-b.txt) y
[`../diagnostics/r2-004a/diagnostic-summary.md`](../diagnostics/r2-004a/diagnostic-summary.md).

**Decisión de ingeniería.** Se reemplazó la forma de unión por un
`LEFT JOIN LATERAL` semánticamente equivalente, preservando paginación,
filtros activos, comportamiento `LEFT JOIN`, campos y orden.

**Hecho medido.** La comparación de equivalencia obtuvo cero diferencias:
`old_minus_new=0`, `new_minus_old=0` y diferencias ordenadas `=0`. El plan
optimizado usa acceso por índice, `Nested Loop Left Join` e `Incremental Sort`;
la ejecución aislada fue 4.417 ms, aproximadamente 82.19 % menor que 24.806
ms. Ver
[`../explain/after-query-b-optimization/comparison.txt`](../explain/after-query-b-optimization/comparison.txt).

**Cautela de atribución.** El mayor número de buffers de ese plan no se
interpreta como una mejora. Tampoco Query B se considera la única causa de
los 503 históricos: A, B y C fallaron juntos en diagnósticos sostenidos.

### Query C — detalles de catálogo

**Hecho medido.** El índice parcial permite `Index Only Scan` en el `COUNT` e
`Index Scan` en la página; el plan posterior no contiene el `Sort` explícito
del plan BEFORE. Ver
[`../explain/before/query-c.txt`](../explain/before/query-c.txt),
[`../explain/after/query-c.txt`](../explain/after/query-c.txt) y
[`../explain/after/comparison.txt`](../explain/after/comparison.txt).

## Índice Liquibase y rollback

**Hecho medido.** La evidencia local confirma la presencia del índice parcial
`ix_catalog_detail_active_catalog_display_order_code` sobre
`parameterization.catalog_detail(catalog_id, display_order, code)` con
predicado `WHERE is_active = true`; ver
[`../explain/after/indexes.txt`](../explain/after/indexes.txt).

**Evidencia de proceso externa a este repositorio.** En el repositorio de base
de datos, el changeset Liquibase correspondiente fue aplicado, sometido a
`rollback-count 1` y reaplicado satisfactoriamente. Esas operaciones no se
repiten ni se modifican desde este repositorio.

## Observaciones diagnósticas históricas

Los diagnósticos de 503 se preservan en
[`../diagnostics/r2-004c/`](../diagnostics/r2-004c/). Dos ejecuciones
sostenidas anteriores mostraron bursts sincronizados de HTTP 503 en A, B y C;
por lo tanto, no justifican atribuir el fenómeno únicamente a Query B.

La observabilidad de errores de repositorio y de `database/sql` se incorporó
para clasificar de forma segura errores de conexión y registrar solo métricas
numéricas del pool. No registra mensajes de error, DSN, direcciones,
credenciales, tokens, headers, SQL, parámetros ni cuerpos.

## Decisión de ingeniería: administración explícita del pool

La única instancia compartida de `database/sql` se configura en
[`../../cmd/reference-data-api/main.go`](../../cmd/reference-data-api/main.go):

| Configuración | Valor |
|---|---:|
| MaxOpenConns | 40 |
| MaxIdleConns | 10 |
| ConnMaxLifetime | 30 min |
| ConnMaxIdleTime | 5 min |

La instrumentación periódica segura de `DB.Stats()` permanece activa en
[`../../internal/adapter/out/postgres/pool_stats.go`](../../internal/adapter/out/postgres/pool_stats.go).
Durante carga diagnóstica con el límite de 40 se observó que el límite estuvo
activo y que `database/sql` aplicó esperas controladas; al terminar, el pool
volvió al conjunto idle configurado.

**Cautela de atribución.** Esta configuración limita la expansión y churn de
conexiones físicas, pero no prueba agotamiento de `max_connections` ni prueba
que el pool por sí solo explique toda la mejora. Los diagnósticos previos
mostraron variabilidad de entorno/runtime e `pgconn.ConnectError`
intermitentes.

## Revisión de contrato público

La revisión del cambio de Query B preserva los campos, filtros, paginación,
orden y comportamiento `LEFT JOIN`, con equivalencia de resultados medida.
La observabilidad devuelve el mismo error al llamador y no modifica el mapeo
HTTP existente. La configuración del pool no altera rutas, autenticación,
autorización ni contratos de respuesta.

## Conclusión

**Conclusión de aceptación:** R2 cumple la prueba final de carga:
**PASS** para `p95 < 300 ms` y **PASS** para errores `< 1 %` bajo 50 VUs
durante cinco minutos.

La mejora final debe entenderse como el resultado combinado de la
optimización SQL/índice y la administración explícita del pool, con las
limitaciones de atribución anteriores. Los diagnósticos históricos permanecen
como evidencia de variabilidad y no se sobrescribieron.

# R2 performance tooling

Este directorio contiene únicamente el dataset y la prueba de baseline de R2.
No forma parte de Liquibase, no se ejecuta con el servicio y no se activa
automáticamente.

## Protección del dataset

El generador exige `R2_ALLOW_DATASET_MUTATION=true` y rechaza cualquier DSN
que no sea exactamente `localhost:15436/design-software-develop`. No usarlo
contra `ds-develop` ni contra `15432`.

El DSN debe suministrarse por variable de entorno y no debe guardarse en Git:

```powershell
$env:R2_DATABASE_URL = 'postgres://<usuario>:<password>@localhost:15436/design-software-develop'
$env:R2_ALLOW_DATASET_MUTATION = 'true'
```

Desde la raíz del repositorio:

```powershell
go run ./performance/dataset -action load -profile smoke
go run ./performance/dataset -action cleanup -profile smoke
go run ./performance/dataset -action load -profile baseline
```

El perfil `smoke` es obligatorio antes de `baseline`. El perfil `baseline`
deja los datos para k6 y las mediciones BEFORE. La limpieza solo elimina filas
con nombres o claves del prefijo `R2 Performance`; nunca usa `TRUNCATE` ni
borra seeds funcionales.

El generador imprime los identificadores de un departamento y un catálogo del
dataset; pásalos a k6 mediante `R2_DEPARTMENT_ID` y `R2_CATALOG_ID`.

## k6

La prueba usa IAM real en `setup()` y no contiene credenciales. Puede recibir
un access token efímero por `K6_ACCESS_TOKEN`, o solicitarlo con:

```powershell
$env:K6_IAM_BASE_URL = 'http://localhost:8081'
$env:K6_IAM_EMAIL = '<usuario autorizado>'
$env:K6_IAM_PASSWORD = '<password suministrado fuera de Git>'
$env:K6_REFERENCE_BASE_URL = 'http://localhost:8080'
$env:R2_DEPARTMENT_ID = '<id del dataset>'
$env:R2_CATALOG_ID = '<id del dataset>'
```

La configuración fija 50 VU durante 5 minutos y mantiene los thresholds:

```text
http_req_duration p(95) < 300 ms
http_req_failed rate < 1 %
```

Si k6 no está instalado localmente, ejecutar con Docker usando el archivo
montado. Desde Docker Desktop, usar `host.docker.internal` como host de los
servicios del equipo anfitrión:

```powershell
docker run --rm -i -v "${PWD}/performance/k6:/scripts" grafana/k6 run /scripts/baseline.js
```

En ese caso configurar `K6_IAM_BASE_URL=http://host.docker.internal:8081` y
`K6_REFERENCE_BASE_URL=http://host.docker.internal:8080`.

Los resultados BEFORE deben guardarse en `performance/results/`, que está
excluido de Git.

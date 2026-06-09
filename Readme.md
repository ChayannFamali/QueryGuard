# QueryGuard

TCP-proxy between applications and PostgreSQL that intercepts SQL queries, analyzes them via AST, applies policies, and collects real-time analytics without requiring application code changes.

```
[App] ──► :5433 [QueryGuard] ──► :5432 [PostgreSQL]
                     │
               [Wire Protocol Parser]
               [SQL AST Analyzer]
               [Policy Engine]  ──► BLOCK / WARN / ALLOW
               [Metrics / Dashboard]
```

## Problem

Developers deploy queries like `SELECT * FROM orders` without `LIMIT`, or N+1 queries that crash the database. The DWH team learns about it when it's already on fire. No one intercepts queries before they hit the database in real time.

---

## How It Works

### TCP Proxy and Wire Protocol

QueryGuard listens on port `:5433` and accepts connections from clients. For each client connection, a second TCP connection is opened to the real PostgreSQL. Each connection is served by a separate goroutine.

Between the client and database goes the binary PostgreSQL Wire Protocol v3. Each message has the structure:

```
┌─────────────┬──────────────────┬─────────────────┐
│type (1 byte)│ length (4 bytes) │  payload        │
└─────────────┴──────────────────┴─────────────────┘
```

The exception is `StartupMessage` (the first message in a connection): it has no type byte. This is critical: the `pgproto3` library adds an extra byte during re-encoding, and PostgreSQL receives invalid data. Therefore, startup is read as raw bytes via `io.ReadFull` and forwarded without transformations.

### Two Goroutines Per Session

Each session launches two independent goroutines:

- `clientToServer` — reads messages from the client, analyzes SQL, applies policy, forwards to postgres or blocks
- `serverToClient` — reads postgres responses, intercepts `CommandComplete` to measure time and row count, forwards to client

Client writes from both goroutines are protected by the `backendMu` mutex; without it, bytes would interleave.

### AST Analysis via pg_query_go

SQL is analyzed via `pg_query_go`, a Go wrapper over the real PostgreSQL C parser. The query is transformed into an abstract syntax tree, which allows precise detection of `SELECT *`, missing `LIMIT`, number of `JOIN`s, and subqueries.

**Performance optimization:** Non-SELECT statements (INSERT, UPDATE, DELETE) skip the full C parser via a fast-path check, reducing CPU overhead.

### Fingerprinting

Queries with different literals (`WHERE id = 1`, `WHERE id = 2`) are normalized to a single pattern (`WHERE id = $1`) and hashed. The result is a fingerprint identical for all structurally identical queries. Used for statistics grouping and N+1 detector.

### N+1 Detector

For each connection, a sliding window is maintained: `map[fingerprint][]timestamp`. If one fingerprint occurs 5 or more times per second (configurable), it's N+1. The alert fires exactly once when the threshold is crossed.

### Query Blocking

When the verdict is `BLOCK`, the query never reaches PostgreSQL. The client receives:

```
ErrorResponse {Code: "57014", Message: "QueryGuard: blocked by policy..."}
ReadyForQuery {TxStatus: 'I'}
```

`ReadyForQuery` is mandatory — without it, the client waits indefinitely for a response.

---

## Features

- Full PostgreSQL Wire Protocol v3 parsing (Simple and Extended Query Protocol)
- SQL AST analysis via the real PostgreSQL parser
- Detectors: `SELECT *`, missing `LIMIT`, high query complexity, N+1
- Query fingerprinting and normalization
- Policy Engine: YAML config, `BLOCK / WARN / ALLOW` actions, `dry_run` mode
- Web dashboard with live query feed (htmx + SSE, no React)
- Prometheus metrics export
- `/health` and `/ready` endpoints for Kubernetes
- **Configurable analyzer thresholds** (N+1 threshold, complexity warning/critical levels)
- **Secure by default**: SQL logging disabled by default, authentication for dashboard and metrics
- **Environment variable overrides** for sensitive credentials

---

## Security Features

### Authentication
- **Dashboard**: Basic Auth with constant-time comparison (when username/password configured)
- **Metrics**: Basic Auth on `/metrics` endpoint; `/health` and `/ready` remain open for Kubernetes probes
- Credentials injected via environment variables (`QG_DASHBOARD_*`, `QG_METRICS_*`)

### Credential Management
- No hardcoded passwords in source control
- `.env` files (gitignored) for local development
- Kubernetes Secrets for production deployments
- `.env.example` provided as a template

### Network Security
- Kubernetes NetworkPolicy restricts pod communication
- ServiceAccount with `automountServiceAccountToken: false`
- Docker Compose network segmentation (db-net, metrics-net)
- PostgreSQL port not exposed to host by default

### Container Hardening
- `securityContext` with `runAsNonRoot`, `readOnlyRootFilesystem`, `drop ALL` capabilities
- Pinned container image versions
- No configs baked into Docker images (mounted at runtime)

### Logging Security
- SQL queries logged only when `log_sql: true` is explicitly set (default: `false`)
- Prevents accidental logging of passwords, PII, or business logic in SQL

---

## Quick Start

### Requirements

- Go 1.22+
- Docker and Docker Compose
- gcc (for CGO — `pg_query_go` uses the PostgreSQL C library)

### Setup

```bash
git clone https://github.com/yourname/queryguard
cd queryguard

# Copy environment template and customize
cp .env.example .env
# Edit .env with secure passwords for local development

# Start PostgreSQL and monitoring stack
make docker-up

# Run the proxy
make run
```

### Configuration

Edit `configs/config.yaml`:

```yaml
proxy:
  listen_addr: "0.0.0.0:5433"
  target_addr: "localhost:5432"

log:
  level: "info"
  format: "console"
  log_sql: false  # Set to true for debugging; false for production

dashboard:
  enabled: true
  listen_addr: "0.0.0.0:8080"
  username: ""  # Override via QG_DASHBOARD_USERNAME env var
  password: ""  # Override via QG_DASHBOARD_PASSWORD env var

policy:
  dry_run: true  # true = only logs, never blocks (safe onboarding)
  config_path: "configs/policies.yaml"

metrics:
  enabled: true
  listen_addr: "0.0.0.0:9090"
  username: ""  # Override via QG_METRICS_USERNAME env var
  password: ""  # Override via QG_METRICS_PASSWORD env var

analyzer:
  n1_threshold: 5      # N+1 alert when same fingerprint appears N times/sec
  complexity_warn: 30  # Complexity score for WARN
  complexity_crit: 60  # Complexity score for CRITICAL
```

Set environment variables to override config:
```bash
export QG_DASHBOARD_USERNAME=admin
export QG_DASHBOARD_PASSWORD=secure-password
export QG_METRICS_USERNAME=metrics
export QG_METRICS_PASSWORD=secure-password
```

### Policies

Edit `configs/policies.yaml`:

```yaml
policies:
  - name: block-missing-limit
    on: [MISSING_LIMIT]
    action: BLOCK
    message: "Add LIMIT to your query to prevent fetching unbounded rows"

  - name: block-select-star
    on: [SELECT_STAR]
    action: BLOCK
    message: "Specify columns explicitly instead of SELECT *"

  - name: warn-n-plus-one
    on: [N_PLUS_ONE]
    action: WARN
    message: "N+1 detected — consider batching queries or using JOINs"

  - name: warn-high-complexity
    on: [HIGH_COMPLEXITY]
    action: WARN
    message: "Query complexity is high — consider simplifying"
```

Policy actions:
- `BLOCK` — query is rejected with an error
- `WARN` — query proceeds but is logged and metric is incremented
- `ALLOW` — query proceeds normally

When `dry_run: true` is set in config, `BLOCK` is downgraded to `WARN` (safe for onboarding).

---

### Testing

```bash
# Connect through the proxy
psql -h localhost -p 5433 -U postgres postgres

# Query without LIMIT will be blocked:
SELECT * FROM orders;
# ERROR: QueryGuard: blocked by policy 'block-missing-limit'

# Proper query will pass:
SELECT id, amount FROM orders LIMIT 10;
```

Dashboard: [http://localhost:8080](http://localhost:8080)

If authentication is enabled, use:
```bash
curl -u admin:secure-password http://localhost:8080
```

---

## Application Listens On

- `:5433` — proxy (connect here instead of postgres directly)
- `:8080` — web dashboard
- `:9090` — Prometheus metrics

---

## Project Structure

```
queryguard/
├── cmd/queryguard/
│   └── main.go
├── internal/
│   ├── config/
│   │   └── config.go         Config structs, env var overrides
│   ├── proxy/
│   │   ├── proxy.go          TCP listener, connection goroutines
│   │   ├── conn.go           Connection handler, postgres retry
│   │   └── session.go        Wire Protocol, analysis, blocking
│   ├── analyzer/
│   │   ├── analyzer.go       Main analyzer, fast-path optimization
│   │   ├── detectors.go      SELECT *, LIMIT, complexity detectors
│   │   ├── result.go         Types: Result, Issue, Severity
│   │   └── n_plus_one.go     N+1 detector with configurable threshold
│   ├── policy/
│   │   └── engine.go         Policy engine, YAML config
│   ├── metrics/
│   │   ├── metrics.go        Prometheus metrics
│   │   └── server.go         HTTP /metrics /health /ready with auth
│   └── dashboard/
│       ├── store.go          Query storage, SSE pub/sub
│       ├── server.go         HTTP dashboard server with auth
│       └── templates/
│           └── index.html    htmx + SSE UI
├── configs/
│   ├── config.yaml
│   └── policies.yaml
├── docker/
│   ├── docker-compose.yml    PostgreSQL, Prometheus, Grafana (with auth)
│   ├── init.sql              Test tables and data
│   └── prometheus.yml
├── k8s/
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── secret.yaml           Dashboard and metrics credentials
│   ├── serviceaccount.yaml   Dedicated SA with no automount
│   ├── networkpolicy.yaml    Restrict pod communication
│   ├── deployment.yaml       Hardened security context
│   └── service.yaml
├── tests/
│   └── integration/
│       └── proxy_test.go
├── .env.example              Environment variable template
├── .gitignore                Ignores .env files
├── Dockerfile
└── Makefile
```

---

## Makefile Commands

```bash
make run               # Run proxy
make build             # Build binary to ./bin/
make test              # Unit tests
make test-integration  # Integration tests (requires running proxy and postgres)
make docker-up         # Start postgres + prometheus + grafana
make docker-down       # Stop containers
make docker-build      # Build Docker image
make psql-proxy        # Connect through proxy
make psql-direct       # Connect directly to postgres
make kill-proxy        # Free port 5433
make k8s-deploy        # Deploy to Kubernetes
make k8s-delete        # Delete Kubernetes namespace
make tidy              # Run go mod tidy
make lint              # Run golangci-lint
```

---

## Metrics

| Metric | Type | Description |
|---|---|---|
| `queryguard_queries_total` | counter | Queries by verdict and protocol |
| `queryguard_blocked_queries_total` | counter | Blocked queries by policy name |
| `queryguard_issues_detected_total` | counter | Issues detected by type |
| `queryguard_query_duration_seconds` | histogram | Query execution time |
| `queryguard_rows_returned` | histogram | Number of rows returned |
| `queryguard_active_connections` | gauge | Current active connections |

---

## Kubernetes Deployment

```bash
# Apply all manifests (namespace, secrets, configmap, deployment, service, network policies)
make k8s-deploy
```

**Important:** Before deploying:
1. Replace placeholders in `k8s/secret.yaml` with real base64-encoded credentials
2. Update `k8s/deployment.yaml` with your container registry and image tag
3. Consider using External Secrets Operator or Sealed Secrets instead of plain Secrets

Proxy is accessible within the cluster as `queryguard-proxy:5433`. Dashboard and metrics are exposed via ClusterIP (use Ingress for external access with TLS).

---

## Docker Deployment

```bash
# Copy and customize environment
cp .env.example .env
# Edit .env with secure passwords

# Start the stack
make docker-up

# Stop the stack
make docker-down

# Remove volumes (deletes database data)
make docker-clean
```

Services:
- PostgreSQL: internal network only (not exposed to host)
- pgAdmin: http://localhost:5050
- Prometheus: http://localhost:9091
- Grafana: http://localhost:3000

---

## Technology Stack

- Go 1.22
- [jackc/pgproto3](https://github.com/jackc/pgx) — PostgreSQL Wire Protocol parser
- [pganalyze/pg_query_go](https://github.com/pganalyze/pg_query_go) — SQL AST via PostgreSQL C parser
- [prometheus/client_golang](https://github.com/prometheus/client_golang) — metrics
- [go.uber.org/zap](https://github.com/uber-go/zap) — structured logging
- htmx + Server-Sent Events — web dashboard without JS frameworks
- gopkg.in/yaml.v3 — policy configuration

---

## Current Limitations

- **TLS not supported**: When a client attempts SSL, the proxy responds with `N` and continues without encryption. In production, place the proxy behind a TLS-terminating reverse proxy or use a sidecar.
- **No connection pooling**: One client connection = one Postgres connection. Under load, this exhausts `max_connections`. Implement a connection pool (e.g., `pgxpool`) or add a PgBouncer sidecar.
- **Statistics in memory**: Proxy restart resets accumulated data.
- **Extended Query Protocol partial support**: `Parse / Bind / Execute` are forwarded transparently, but only SQL from `Parse` is analyzed.

---

## Security Best Practices

1. **Never commit `.env` files** — they are gitignored by default
2. **Use strong passwords** for dashboard and metrics authentication
3. **Set `log_sql: false`** in production to prevent SQL with sensitive data from being logged
4. **Use Kubernetes Secrets** (or External Secrets) instead of ConfigMaps for credentials
5. **Enable NetworkPolicy** to restrict pod-to-pod communication
6. **Pin container image tags** to avoid unexpected updates
7. **Run with non-root user** (already configured in Dockerfile and K8s deployment)
8. **Use TLS termination** in production (Ingress controller or sidecar)

---

## License

MIT

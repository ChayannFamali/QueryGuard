# QueryGuard — Complete Usage Guide

This document provides step-by-step instructions for installing, configuring, running, and using QueryGuard from start to finish.

---

## Table of Contents

1. [Installation](#installation)
2. [Local Development Setup](#local-development-setup)
3. [Configuration](#configuration)
4. [Running the Proxy](#running-the-proxy)
5. [Using the Dashboard](#using-the-dashboard)
6. [Policy Configuration](#policy-configuration)
7. [Testing and Validation](#testing-and-validation)
8. [Docker Deployment](#docker-deployment)
9. [Kubernetes Deployment](#kubernetes-deployment)
10. [Monitoring and Metrics](#monitoring-and-metrics)
11. [Troubleshooting](#troubleshooting)
12. [Advanced Usage](#advanced-usage)

---

## Installation

### Prerequisites

Ensure you have the following installed on your system:

```bash
# Go 1.22 or higher
go version
# Expected output: go version go1.22.x linux/amd64 (or similar)

# Docker and Docker Compose
docker --version
docker compose version

# gcc (required for CGO - pg_query_go uses PostgreSQL C parser)
gcc --version

# psql client (optional, for testing)
psql --version
```

### Install Go (if not installed)

```bash
# Ubuntu/Debian
wget https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify
go version
```

### Install Docker (if not installed)

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Add your user to docker group
sudo usermod -aG docker $USER
newgrp docker

# Verify
docker --version
docker compose version
```

### Install gcc (if not installed)

```bash
# Ubuntu/Debian
sudo apt-get install -y build-essential

# Verify
gcc --version
```

---

## Local Development Setup

### Step 1: Clone the Repository

```bash
git clone https://github.com/yourname/queryguard.git
cd queryguard
```

### Step 2: Set Up Environment Variables

```bash
# Copy the example environment file
cp .env.example .env

# Edit .env with your preferred local development passwords
nano .env  # or use your preferred editor
```

Example `.env` content:
```bash
POSTGRES_PASSWORD=my-local-pg-password
PGADMIN_DEFAULT_PASSWORD=my-local-pgadmin-password
GF_SECURITY_ADMIN_PASSWORD=my-local-grafana-password
QG_DASHBOARD_USERNAME=admin
QG_DASHBOARD_PASSWORD=my-secure-dashboard-password
QG_METRICS_USERNAME=metrics
QG_METRICS_PASSWORD=my-secure-metrics-password
```

**Important:** Never commit your `.env` file to version control. It's already in `.gitignore`.

### Step 3: Start Infrastructure Services

```bash
# Start PostgreSQL, Prometheus, and Grafana in the background
make docker-up
```

Verify services are running:
```bash
docker ps
```

Expected output:
```
CONTAINER ID   IMAGE                    STATUS         PORTS
queryguard-postgres    postgres:16-alpine     Up (healthy)   5432/tcp
queryguard-prometheus  prom/prometheus        Up             0.0.0.0:9091->9090/tcp
queryguard-grafana     grafana/grafana        Up             0.0.0.0:3000->3000/tcp
queryguard-pgadmin     dpage/pgadmin4         Up             0.0.0.0:5050->80/tcp
```

### Step 4: Install Go Dependencies

```bash
# Download and verify dependencies
go mod download
go mod verify
```

### Step 5: Build the Binary

```bash
make build
```

The binary will be created at `./bin/queryguard`.

### Step 6: Run Tests

```bash
# Run unit tests
make test

# Run tests with coverage report
make test-cover
```

---

## Configuration

QueryGuard uses YAML configuration files located in the `configs/` directory.

### Main Configuration: `configs/config.yaml`

Edit this file to customize QueryGuard behavior:

```yaml
proxy:
  listen_addr: "0.0.0.0:5433"      # Address to listen for client connections
  target_addr: "localhost:5432"     # PostgreSQL backend address

log:
  level: "info"                     # debug | info | warn | error
  format: "console"                 # console | json
  log_sql: false                    # Set to true for debugging; false for production

dashboard:
  enabled: true
  listen_addr: "0.0.0.0:8080"
  username: ""                      # Override via QG_DASHBOARD_USERNAME
  password: ""                      # Override via QG_DASHBOARD_PASSWORD

policy:
  dry_run: true                     # true = only logs, never blocks (safe onboarding)
  config_path: "configs/policies.yaml"

metrics:
  enabled: true
  listen_addr: "0.0.0.0:9090"
  username: ""                      # Override via QG_METRICS_USERNAME
  password: ""                      # Override via QG_METRICS_PASSWORD

analyzer:
  n1_threshold: 5                   # N+1 alert when same fingerprint appears N times/sec
  complexity_warn: 30               # Complexity score for WARN
  complexity_crit: 60               # Complexity score for CRITICAL
```

### Setting Credentials via Environment Variables

Instead of putting passwords in config files, use environment variables:

```bash
export QG_DASHBOARD_USERNAME=admin
export QG_DASHBOARD_PASSWORD=your-secure-password
export QG_METRICS_USERNAME=metrics
export QG_METRICS_PASSWORD=your-secure-metrics-password
```

Then run QueryGuard:
```bash
make run
```

### Configuration Options Explained

#### Proxy Section
- **listen_addr**: The address and port where QueryGuard accepts client connections
- **target_addr**: The PostgreSQL server address to forward queries to

#### Log Section
- **level**: Controls verbosity. Use `debug` for development, `info` or higher for production
- **format**: `console` (human-readable) or `json` (structured logging)
- **log_sql**: When `false` (default), SQL queries are NOT logged, protecting sensitive data. Set to `true` only for debugging

#### Dashboard Section
- **enabled**: Enable/disable the web dashboard
- **username/password**: Basic Auth credentials. If both are empty, dashboard is accessible without authentication (not recommended for production)

#### Policy Section
- **dry_run**: When `true`, policies only log but don't block queries. Recommended for initial deployment
- **config_path**: Path to the policies YAML file

#### Metrics Section
- **enabled**: Enable/disable Prometheus metrics endpoint
- **username/password**: Basic Auth for `/metrics`. Health probes remain open

#### Analyzer Section
- **n1_threshold**: Number of times the same query fingerprint must appear in 1 second to trigger N+1 alert
- **complexity_warn**: Complexity score threshold for warnings
- **complexity_crit**: Complexity score threshold for critical alerts

---

## Running the Proxy

### Method 1: Using Make (Development)

```bash
make run
```

This command:
1. Frees port 5433 if it's in use
2. Runs the proxy with `go run`
3. Uses `configs/config.yaml` by default

### Method 2: Running the Binary (Production)

```bash
# Build first
make build

# Run the binary
./bin/queryguard -config configs/config.yaml
```

### Method 3: With Custom Config Path

```bash
./bin/queryguard -config /path/to/custom/config.yaml
```

### Method 4: With Environment Variables

```bash
export QG_DASHBOARD_USERNAME=admin
export QG_DASHBOARD_PASSWORD=secure-password
export QG_METRICS_USERNAME=metrics
export QG_METRICS_PASSWORD=secure-password

./bin/queryguard -config configs/config.yaml
```

### Expected Output

When QueryGuard starts successfully, you'll see:

```
QueryGuard v0.0.1 starting...
2026-06-09T10:00:00.000+0000    INFO    config loaded   {"listen_addr": "0.0.0.0:5433", "target_addr": "localhost:5432", "dry_run": true}
2026-06-09T10:00:00.001+0000    INFO    metrics server listening        {"addr": "0.0.0.0:9090", "metrics_url": "http://0.0.0.0:9090/metrics", "auth_enabled": true}
2026-06-09T10:00:00.002+0000    INFO    dashboard listening     {"addr": "0.0.0.0:8080", "url": "http://0.0.0.0:8080", "auth_enabled": true}
2026-06-09T10:00:00.003+0000    INFO    proxy listening       {"addr": "0.0.0.0:5433", "forwarding_to": "localhost:5432", "dry_run": true}
2026-06-09T10:00:00.004+0000    INFO    policies loaded {"count": 4}
```

### Verify the Proxy is Running

```bash
# Check if port 5433 is listening
ss -tlnp | grep 5433

# Or
netstat -tlnp | grep 5433
```

---

## Using the Dashboard

### Access the Dashboard

Open your browser and navigate to:
```
http://localhost:8080
```

If authentication is enabled, you'll see a browser prompt for username and password. Enter the credentials from your config or environment variables.

### Dashboard Features

1. **Live Query Feed**: Real-time display of all queries passing through the proxy
2. **Statistics**: Total queries, blocked queries, warnings, allowed queries
3. **Query Details**: Click on a query to see:
   - SQL text (if `log_sql: true`)
   - Fingerprint
   - Verdict (allow/warn/block)
   - Applied policy
   - Duration
   - Rows returned
   - Detected issues

### Dashboard API Endpoints

```bash
# Main dashboard page
curl -u admin:password http://localhost:8080/

# Server-Sent Events stream (live updates)
curl -u admin:password http://localhost:8080/events

# Partial stats update (for AJAX refresh)
curl -u admin:password http://localhost:8080/partial/stats
```

---

## Policy Configuration

### Policies File: `configs/policies.yaml`

This file defines what queries to block, warn, or allow.

### Available Issue Types

- **SELECT_STAR**: Query uses `SELECT *`
- **MISSING_LIMIT**: SELECT query without LIMIT clause
- **HIGH_COMPLEXITY**: Query complexity exceeds threshold
- **N_PLUS_ONE**: Same query fingerprint executed multiple times per second

### Policy Actions

- **BLOCK**: Reject the query with an error
- **WARN**: Allow the query but log and increment counter
- **ALLOW**: Allow the query (default for queries without issues)

### Example Policy Configuration

```yaml
policies:
  # Block queries without LIMIT
  - name: block-missing-limit
    description: "SELECT without LIMIT can return millions of rows"
    on: [MISSING_LIMIT]
    action: BLOCK
    message: "Add LIMIT to your query to prevent fetching unbounded rows"

  # Block SELECT *
  - name: block-select-star
    description: "SELECT * fetches unnecessary columns"
    on: [SELECT_STAR]
    action: BLOCK
    message: "Specify columns explicitly instead of SELECT *"

  # Warn about N+1 queries
  - name: warn-n-plus-one
    description: "N+1 pattern kills database performance"
    on: [N_PLUS_ONE]
    action: WARN
    message: "N+1 detected — consider batching queries or using JOINs"

  # Warn about complex queries
  - name: warn-high-complexity
    description: "Complex queries should be reviewed"
    on: [HIGH_COMPLEXITY]
    action: WARN
    message: "Query complexity is high — consider simplifying"
```

### Policy Priority

When multiple policies match, the priority is:
1. **BLOCK** (highest priority)
2. **WARN**
3. **ALLOW** (if no issues detected)

### Dry Run Mode

When `policy.dry_run: true` in `config.yaml`, all `BLOCK` actions are downgraded to `WARN`. This is useful for:
- Initial deployment without breaking existing applications
- Monitoring what would be blocked before enforcing
- Gradual onboarding of applications

### Reloading Policies

Policies are loaded at startup. To reload after changes:
```bash
# Restart the proxy
make kill-proxy
make run
```

(Future versions may support hot-reload via SIGHUP)

---

## Testing and Validation

### Connect to the Proxy

```bash
# Using psql directly
psql -h localhost -p 5433 -U postgres postgres

# Or using make target
make psql-proxy
```

### Test Query Blocking

```sql
-- This should be BLOCKED (no LIMIT)
SELECT * FROM users;
-- ERROR:  QueryGuard: blocked by policy 'block-missing-limit'
-- DETAIL:  Add LIMIT to your query to prevent fetching unbounded rows
-- HINT:  Modify your query to comply with the database access policy

-- This should be BLOCKED (SELECT *)
SELECT * FROM orders LIMIT 10;
-- ERROR: QueryGuard: blocked by policy 'block-select-star'

-- This should be ALLOWED
SELECT id, email FROM users LIMIT 10;
-- Works normally

-- This should be ALLOWED (aggregate without LIMIT is OK)
SELECT count(*) FROM users;
-- Works normally
```

### Test N+1 Detection

In your application (or a script), execute the same query multiple times rapidly:

```python
# Example Python script
import psycopg2

conn = psycopg2.connect("host=localhost port=5433 user=postgres dbname=postgres")
cursor = conn.cursor()

# Execute the same query 6 times in rapid succession
for i in range(6):
    cursor.execute("SELECT id FROM users WHERE id = %s", (i,))
    cursor.fetchall()

# After the 5th execution, you'll see a WARN in the logs:
# "possible N+1: same query pattern executed many times in 1 second"
```

Check the dashboard — the N+1 issue will be highlighted.

### Run Integration Tests

```bash
# Start the proxy in one terminal
make run

# In another terminal, run integration tests
make test-integration
```

Integration tests verify:
- Blocked queries return correct errors
- Allowed queries work normally
- Transactions work through the proxy

### Health Checks

```bash
# Health endpoint (for liveness probes)
curl http://localhost:9090/health
# Output: ok

# Ready endpoint (for readiness probes)
curl http://localhost:9090/ready
# Output: ok
```

---

## Docker Deployment

### Build the Docker Image

```bash
make docker-build
```

Or manually:
```bash
docker build -t queryguard:0.0.1 .
```

### Run with Docker Compose

The included `docker/docker-compose.yml` starts the full stack:

```bash
# Copy and customize environment
cp .env.example .env
nano .env  # Set your passwords

# Start everything
make docker-up

# View logs
docker compose -f docker/docker-compose.yml logs -f

# Stop
make docker-down

# Remove volumes (deletes database)
make docker-clean
```

### Services Started

- **PostgreSQL**: Internal network only (not exposed to host)
- **QueryGuard Proxy**: `localhost:5433`
- **Dashboard**: `http://localhost:8080`
- **Metrics**: `http://localhost:9090`
- **pgAdmin**: `http://localhost:5050`
- **Prometheus**: `http://localhost:9091`
- **Grafana**: `http://localhost:3000`

### Connect Applications to Docker Proxy

Update your application's database connection string:

```
# Before (direct to Postgres)
postgres://user:pass@localhost:5432/mydb

# After (through QueryGuard)
postgres://user:pass@localhost:5433/mydb
```

---

## Kubernetes Deployment

### Prerequisites

- Kubernetes cluster (v1.24+)
- `kubectl` configured to access your cluster
- Container registry access (Docker Hub, ECR, GCR, etc.)

### Step 1: Build and Push Image

```bash
# Build with your registry
export REGISTRY=your-registry.com
export IMAGE_TAG=0.0.1

docker build -t ${REGISTRY}/queryguard:${IMAGE_TAG} .
docker push ${REGISTRY}/queryguard:${IMAGE_TAG}
```

### Step 2: Update Deployment Manifest

Edit `k8s/deployment.yaml`:

```yaml
spec:
  containers:
    - name: queryguard
      image: your-registry.com/queryguard:0.0.1  # <-- Change this
      imagePullPolicy: Always
```

### Step 3: Create Secrets

Edit `k8s/secret.yaml` with real base64-encoded values:

```bash
# Encode your secrets
echo -n "admin" | base64
echo -n "your-secure-dashboard-password" | base64
echo -n "metrics" | base64
echo -n "your-secure-metrics-password" | base64
```

Update `k8s/secret.yaml`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: queryguard-secret
  namespace: queryguard
type: Opaque
stringData:
  dashboard-username: "admin"
  dashboard-password: "your-secure-dashboard-password"
  metrics-username: "metrics"
  metrics-password: "your-secure-metrics-password"
```

**Production Tip:** Use External Secrets Operator or Sealed Secrets instead of plain Secrets in version control.

### Step 4: Deploy to Kubernetes

```bash
# Apply all manifests
make k8s-deploy
```

Or manually:
```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/secret.yaml
kubectl apply -f k8s/serviceaccount.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/networkpolicy.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml

# Wait for rollout
kubectl rollout status deployment/queryguard -n queryguard
```

### Step 5: Verify Deployment

```bash
# Check pods
kubectl get pods -n queryguard

# Check services
kubectl get services -n queryguard

# Check logs
kubectl logs -n queryguard -l app=queryguard -f
```

### Step 6: Access from Application

Update your application's database connection to use the Kubernetes service:

```yaml
# In your application's deployment
env:
  - name: DATABASE_URL
    value: "postgres://user:pass@queryguard-proxy:5433/mydb"
```

### Step 7: Expose Dashboard (Optional)

Create an Ingress for external access:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: queryguard-dashboard
  namespace: queryguard
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/auth-type: basic
    nginx.ingress.kubernetes.io/auth-secret: queryguard-dashboard-auth
spec:
  tls:
    - hosts:
        - queryguard.example.com
      secretName: queryguard-tls
  rules:
    - host: queryguard.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: queryguard-dashboard
                port:
                  number: 8080
```

### Remove from Kubernetes

```bash
make k8s-delete
# Or: kubectl delete namespace queryguard
```

---

## Monitoring and Metrics

### Prometheus Metrics

QueryGuard exposes metrics at `http://localhost:9090/metrics`.

If authentication is enabled:
```bash
curl -u metrics:password http://localhost:9090/metrics
```

### Available Metrics

```
# HELP queryguard_queries_total Total queries processed by verdict and protocol
# TYPE queryguard_queries_total counter
queryguard_queries_total{protocol="simple",verdict="allow"} 1234
queryguard_queries_total{protocol="extended",verdict="block"} 56

# HELP queryguard_blocked_queries_total Total blocked queries by policy name
# TYPE queryguard_blocked_queries_total counter
queryguard_blocked_queries_total{policy="block-missing-limit"} 42

# HELP queryguard_issues_detected_total Total issues detected by type
# TYPE queryguard_issues_detected_total counter
queryguard_issues_detected_total{issue_type="SELECT_STAR"} 15

# HELP queryguard_query_duration_seconds Query duration from proxy perspective
# TYPE queryguard_query_duration_seconds histogram
queryguard_query_duration_seconds_bucket{verdict="allow",le="0.001"} 100
queryguard_query_duration_seconds_bucket{verdict="allow",le="0.01"} 200

# HELP queryguard_rows_returned Number of rows returned per query
# TYPE queryguard_rows_returned histogram
queryguard_rows_returned_bucket{verdict="allow",le="10"} 500

# HELP queryguard_active_connections Number of active client connections
# TYPE queryguard_active_connections gauge
queryguard_active_connections 5
```

### Configure Prometheus Scraping

Edit `docker/prometheus.yml`:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'queryguard'
    basic_auth:
      username: 'metrics'
      password: 'your-secure-metrics-password'
    static_configs:
      - targets: ['host.docker.internal:9090']
```

For Kubernetes, the deployment already includes Prometheus annotations:
```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "9090"
  prometheus.io/path: "/metrics"
```

### Grafana Dashboard

1. Access Grafana at `http://localhost:3000` (default login: admin/your-grafana-password)
2. Add Prometheus data source:
   - URL: `http://prometheus:9090`
3. Import or create dashboard with panels for:
   - Query rate by verdict
   - Blocked queries by policy
   - Query duration histogram
   - Active connections
   - Issues detected over time

### Alert Examples

Configure alerts in Prometheus or Grafana:

```yaml
# Prometheus alerting rules
groups:
  - name: queryguard
    rules:
      - alert: HighBlockRate
        expr: rate(queryguard_blocked_queries_total[5m]) > 10
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "High query block rate"
          description: "More than 10 queries per second are being blocked"

      - alert: NPlusOneDetected
        expr: increase(queryguard_issues_detected_total{issue_type="N_PLUS_ONE"}[1h]) > 0
        labels:
          severity: info
        annotations:
          summary: "N+1 query pattern detected"
```

---

## Troubleshooting

### Port Already in Use

```bash
# Check what's using port 5433
sudo lsof -i :5433

# Kill the process
make kill-proxy

# Or manually
sudo fuser -k 5433/tcp
```

### Can't Connect to Proxy

**Check 1: Is the proxy running?**
```bash
ps aux | grep queryguard
# Or check logs
```

**Check 2: Is PostgreSQL accessible?**
```bash
psql -h localhost -p 5432 -U postgres postgres
```

**Check 3: Check proxy logs**
```bash
# Look for errors in the QueryGuard output
```

**Check 4: Firewall**
```bash
# Allow port 5433 (if using ufw)
sudo ufw allow 5433/tcp
```

### Queries Not Being Blocked

**Check 1: Is `dry_run` enabled?**
```yaml
policy:
  dry_run: true  # Change to false to enforce blocking
```

**Check 2: Are policies loaded?**
Check logs for:
```
policies loaded {"count": 4}
```

**Check 3: Does the policy match your issue?**
Review `configs/policies.yaml` to ensure the `on:` field includes the issue type you're testing.

### Dashboard Returns 401 Unauthorized

**Check 1: Are you providing credentials?**
```bash
curl -u username:password http://localhost:8080/
```

**Check 2: Are credentials configured?**
```bash
export QG_DASHBOARD_USERNAME=admin
export QG_DASHBOARD_PASSWORD=your-password
# Restart the proxy
```

### High CPU Usage

**Cause:** AST parsing every query is CPU-intensive.

**Solution 1: Increase log level**
```yaml
log:
  level: "warn"  # Reduce logging overhead
```

**Solution 2: Tune complexity thresholds**
```yaml
analyzer:
  complexity_warn: 50  # Increase if you have legitimate complex queries
  complexity_crit: 100
```

**Solution 3: Use connection pooling**
Add PgBouncer between QueryGuard and PostgreSQL to reduce connection overhead.

### Memory Issues

**Cause:** Query entries are kept in memory (last 500 queries).

**Solutions:**
- This is by design for the dashboard. Restart the proxy to clear.
- For production, ensure adequate memory limits (256Mi in K8s config is sufficient).

### CGO Build Errors

```bash
# Ensure gcc is installed
gcc --version

# Install build tools
sudo apt-get install build-essential

# Build with explicit CGO
CGO_ENABLED=1 go build ./...
```

### Docker Compose Errors

```bash
# Check if Docker is running
docker ps

# Restart Docker
sudo systemctl restart docker

# Remove and recreate
make docker-clean
make docker-up
```

### Kubernetes Pod Crashes

```bash
# Check pod status
kubectl get pods -n queryguard

# Check logs
kubectl logs -n queryguard -l app=queryguard --previous

# Check events
kubectl describe pod -n queryguard -l app=queryguard
```

**Common issues:**
- Image not found: Update `image:` in deployment.yaml
- ConfigMap not found: Apply configmap.yaml before deployment
- Secret not found: Apply secret.yaml before deployment
- Health check failing: Ensure PostgreSQL is reachable from the pod

---

## Advanced Usage

### Custom Policy Examples

**Block queries with too many JOINs:**
```yaml
- name: block-complex-joins
  on: [HIGH_COMPLEXITY]
  action: BLOCK
  message: "Query has too many JOINs — consider breaking into subqueries"
```

**Warn about specific patterns:**
```yaml
- name: warn-select-star-on-large-tables
  on: [SELECT_STAR]
  action: WARN
  message: "SELECT * detected on potentially large table"
```

### Multiple Proxy Instances

Run multiple QueryGuard instances for different databases:

```bash
# Instance 1
./bin/queryguard -config configs/config-db1.yaml

# Instance 2
./bin/queryguard -config configs/config-db2.yaml
```

Each instance can have different policies, target addresses, and ports.

### Integration with Application Frameworks

**Node.js (pg):**
```javascript
const { Client } = require('pg');
const client = new Client({
  host: 'localhost',
  port: 5433,  // QueryGuard port
  user: 'postgres',
  password: 'password',
  database: 'mydb'
});
```

**Python (psycopg2):**
```python
import psycopg2
conn = psycopg2.connect(
    host='localhost',
    port=5433,  # QueryGuard port
    user='postgres',
    password='password',
    dbname='mydb'
)
```

**Java (JDBC):**
```java
String url = "jdbc:postgresql://localhost:5433/mydb";
Connection conn = DriverManager.getConnection(url, "postgres", "password");
```

**Ruby (pg):**
```ruby
require 'pg'
conn = PG.connect(
  host: 'localhost',
  port: 5433,  # QueryGuard port
  user: 'postgres',
  password: 'password',
  dbname: 'mydb'
)
```

### Gradual Rollout Strategy

1. **Phase 1: Observation (Week 1)**
   ```yaml
   policy:
     dry_run: true  # All policies in warn mode
   ```
   Monitor the dashboard to understand query patterns.

2. **Phase 2: Selective Enforcement (Week 2-3)**
   ```yaml
   policies:
     - name: block-missing-limit
       on: [MISSING_LIMIT]
       action: BLOCK  # Enforce this one first
     - name: warn-select-star
       on: [SELECT_STAR]
       action: WARN  # Keep as warning
   ```

3. **Phase 3: Full Enforcement (Week 4+)**
   ```yaml
   policy:
     dry_run: false
   policies:
     - name: block-missing-limit
       action: BLOCK
     - name: block-select-star
       action: BLOCK
   ```

### Log Analysis

When `log_sql: true` is set (for debugging only), extract SQL from logs:

```bash
# Extract all blocked queries
grep "BLOCKED" queryguard.log | jq -r '.sql' > blocked_queries.sql

# Find queries with high complexity
grep "high_complexity" queryguard.log | jq -r '{sql, complexity}' | sort -k2 -nr

# N+1 patterns
grep "N_PLUS_ONE" queryguard.log | jq -r '.fingerprint' | sort | uniq -c
```

**Important:** Disable `log_sql` in production to avoid logging sensitive data.

### Performance Tuning

**Increase N+1 threshold if you have legitimate batch operations:**
```yaml
analyzer:
  n1_threshold: 10  # Only alert if same query appears 10+ times/sec
```

**Reduce logging overhead:**
```yaml
log:
  level: "warn"
  format: "json"
  log_sql: false
```

**Tune complexity for your workload:**
```yaml
analyzer:
  complexity_warn: 40  # If you have legitimate complex reporting queries
  complexity_crit: 80
```

---

## Support and Resources

- **Source Code**: [https://github.com/yourname/queryguard](https://github.com/yourname/queryguard)
- **Issues**: [https://github.com/yourname/queryguard/issues](https://github.com/yourname/queryguard/issues)
- **Documentation**: This file and `Readme.md`
- **License**: MIT

---

## Quick Reference

### Common Commands

```bash
# Start infrastructure
make docker-up

# Run proxy
make run

# Connect to proxy
make psql-proxy

# View logs
docker compose -f docker/docker-compose.yml logs -f queryguard

# Run tests
make test
make test-integration

# Build binary
make build

# Build Docker image
make docker-build

# Deploy to Kubernetes
make k8s-deploy

# Stop everything
make kill-proxy
make docker-down
```

### Port Summary

| Service | Port | Protocol |
|---------|------|----------|
| QueryGuard Proxy | 5433 | TCP (PostgreSQL Wire Protocol) |
| Dashboard | 8080 | HTTP |
| Metrics | 9090 | HTTP |
| pgAdmin | 5050 | HTTP |
| Prometheus | 9091 | HTTP |
| Grafana | 3000 | HTTP |

### Environment Variables

| Variable | Purpose |
|----------|---------|
| `QG_DASHBOARD_USERNAME` | Dashboard Basic Auth username |
| `QG_DASHBOARD_PASSWORD` | Dashboard Basic Auth password |
| `QG_METRICS_USERNAME` | Metrics Basic Auth username |
| `QG_METRICS_PASSWORD` | Metrics Basic Auth password |
| `PROXY_DSN` | Integration test connection string |
| `POSTGRES_PASSWORD` | PostgreSQL password (Docker) |
| `PGADMIN_DEFAULT_PASSWORD` | pgAdmin password (Docker) |
| `GF_SECURITY_ADMIN_PASSWORD` | Grafana admin password (Docker) |

---

**End of Guide**

For questions or contributions, please open an issue on GitHub.

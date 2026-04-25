# QueryGuard

TCP-прокси между приложением и PostgreSQL, который перехватывает SQL-запросы, анализирует их через AST, применяет политики и собирает аналитику в реальном времени, без изменений в коде приложения.

```
[App] ──► :5433 [QueryGuard] ──► :5432 [PostgreSQL]
                     │
               [Wire Protocol Parser]
               [SQL AST Analyzer]
               [Policy Engine]  ──► BLOCK / WARN / ALLOW
               [Metrics / Dashboard]
```

## Проблема

Разработчики деплоят запросы вида `SELECT * FROM orders` без `LIMIT`, или N+1 запросы, которые кладут базу. DWH-команда узнает об этом когда уже горит. Никто не перехватывает запросы до базы в реальном времени.

---

## Как это работает

### TCP-прокси и Wire Protocol

QueryGuard слушает порт `:5433` и принимает соединения от клиентов. Для каждого клиентского соединения открывается второе TCP-соединение к реальному PostgreSQL. Каждое соединение обслуживается отдельной горутиной.

Между клиентом и базой данных идёт бинарный PostgreSQL Wire Protocol v3. Каждое сообщение имеет структуру:

```
┌─────────────┬──────────────────┬─────────────────┐
│type (1 byte)│ length (4 bytes) │  payload        │
└─────────────┴──────────────────┴─────────────────┘
```

Исключением является `StartupMessage` (первое сообщение соединения): у него нет type-байта. Это критично: библиотека `pgproto3` при ре-кодировании добавляет лишний байт, и PostgreSQL получает невалидные данные. Поэтому startup читается сырыми байтами через `io.ReadFull` и пересылается без трансформаций.

### Две горутины на сессию

Каждая сессия запускает две независимые горутины:

- `clientToServer` — читает сообщения от клиента, анализирует SQL, применяет политику, пересылает в postgres или блокирует
- `serverToClient` — читает ответы postgres, перехватывает `CommandComplete` для замера времени и количества строк, пересылает клиенту

Запись клиенту из обеих горутин защищена мьютексом `backendMu`, без него байты перемешаются.

### AST-анализ через pg_query_go

SQL анализируется через `pg_query_go` Go-обёртку над реальным C-парсером PostgreSQL. Запрос превращается в абстрактное синтаксическое дерево, по которому можно точно определить наличие `SELECT *`, отсутствие `LIMIT`, количество `JOIN`-ов и подзапросов.

### Fingerprinting

Запросы с разными литералами (`WHERE id = 1`, `WHERE id = 2`) нормализуются в один паттерн (`WHERE id = $1`) и хэшируются. Результат — fingerprint, одинаковый для всех структурно идентичных запросов. Используется для группировки статистики и работы N+1 детектора.

### N+1 детектор

Для каждого соединения ведётся скользящее окно: `map[fingerprint][]timestamp`. Если один fingerprint встречается 5 и более раз за 1 секунду — это N+1. Алерт срабатывает ровно один раз при пересечении порога.

### Блокировка запроса

При вердикте `BLOCK` запрос не доходит до PostgreSQL. Клиент получает:

```
ErrorResponse {Code: "57014", Message: "QueryGuard: blocked by policy..."}
ReadyForQuery {TxStatus: 'I'}
```

`ReadyForQuery` обязателен — без него клиент ждёт ответа бесконечно.

---

## Возможности

- Полный парсинг PostgreSQL Wire Protocol v3 (Simple и Extended Query Protocol)
- SQL AST-анализ через реальный парсер PostgreSQL
- Детекторы: `SELECT *`, отсутствие `LIMIT`, высокая сложность запроса, N+1
- Query fingerprinting и нормализация запросов
- Policy Engine: YAML-конфиг, действия `BLOCK / WARN / ALLOW`, режим `dry_run`
- Веб-дашборд с live-лентой запросов (htmx + SSE, без React)
- Экспорт метрик в Prometheus
- Эндпоинты `/health` и `/ready` для Kubernetes

---

## Быстрый старт

### Требования

- Go 1.22+
- Docker и Docker Compose
- gcc (для CGO — `pg_query_go` использует C-библиотеку PostgreSQL)

### Запуск

```bash
git clone https://github.com/yourname/queryguard
cd queryguard

# Поднять PostgreSQL локально
make docker-up

# Запустить прокси
make run
make psql-proxy
```

Приложение слушает:
- `:5433` — прокси (вместо прямого подключения к postgres)
- `:8080` — веб-дашборд
- `:9090` — метрики Prometheus

### Проверка

```bash
# Подключение через прокси
psql -h localhost -p 5433 -U postgres postgres

# Запрос без LIMIT будет заблокирован:
SELECT * FROM orders;
# ERROR: QueryGuard: blocked by policy 'block-missing-limit'

# Правильный запрос пройдёт:
SELECT id, amount FROM orders LIMIT 10;
```

Дашборд: [http://localhost:8080](http://localhost:8080)

---

## Конфигурация

### `configs/config.yaml`

```yaml
proxy:
  listen_addr: "0.0.0.0:5433"
  target_addr: "localhost:5432"

log:
  level: "info"     # debug | info | warn | error
  format: "console" # console | json

dashboard:
  enabled: true
  listen_addr: "0.0.0.0:8080"

policy:
  dry_run: true     # true — только логирует, не блокирует
  config_path: "configs/policies.yaml"

metrics:
  enabled: true
  listen_addr: "0.0.0.0:9090"
```

### `configs/policies.yaml`

```yaml
policies:
  - name: block-missing-limit
    on: [MISSING_LIMIT]
    action: BLOCK
    message: "Add LIMIT to your query to prevent fetching unbounded rows"

  - name: warn-select-star
    on: [SELECT_STAR]
    action: WARN
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

Изменения в `policies.yaml` применяются перезапуском. Поле `dry_run: true` означает что политики только логируют — ничего не блокируется. Используется для безопасного онбординга.

---

## Структура проекта

```
queryguard/
├── cmd/queryguard/
│   └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── proxy/
│   │   ├── proxy.go        TCP listener, горутины соединений
│   │   ├── conn.go         обработчик соединения, retry к postgres
│   │   └── session.go      Wire Protocol, анализ, блокировка
│   ├── analyzer/
│   │   ├── analyzer.go     главный анализатор
│   │   ├── detectors.go    SELECT *, LIMIT, complexity
│   │   ├── result.go       типы: Result, Issue, Severity
│   │   └── n_plus_one.go   N+1 детектор
│   ├── policy/
│   │   └── engine.go       движок политик, YAML-конфиг
│   ├── metrics/
│   │   ├── metrics.go      Prometheus метрики
│   │   └── server.go       HTTP /metrics /health /ready
│   └── dashboard/
│       ├── store.go        хранилище запросов, SSE pub/sub
│       ├── server.go       HTTP сервер дашборда
│       └── templates/
│           └── index.html  htmx + SSE UI
├── configs/
│   ├── config.yaml
│   └── policies.yaml
├── docker/
│   ├── docker-compose.yml  PostgreSQL, Prometheus, Grafana
│   ├── init.sql            тестовые таблицы и данные
│   └── prometheus.yml
├── k8s/
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   ├── deployment.yaml
│   └── service.yaml
├── tests/
│   └── integration/
│       └── proxy_test.go
├── Dockerfile
└── Makefile
```

---

## Команды Makefile

```bash
make run               # запуск прокси
make build             # сборка бинарника в ./bin/
make test              # unit-тесты
make test-integration  # интеграционные тесты (требует запущенного прокси)
make docker-up         # поднять postgres + prometheus + grafana
make docker-down       # остановить контейнеры
make docker-build      # собрать Docker образ
make psql-proxy        # подключиться через прокси
make psql-direct       # подключиться напрямую к postgres
make kill-proxy        # освободить порт 5433
```

---

## Метрики

| Метрика | Тип | Описание |
|---|---|---|
| `queryguard_queries_total` | counter | Запросы по verdict и protocol |
| `queryguard_blocked_queries_total` | counter | Заблокированные запросы по имени политики |
| `queryguard_issues_detected_total` | counter | Найденные проблемы по типу |
| `queryguard_query_duration_seconds` | histogram | Время выполнения запроса |
| `queryguard_rows_returned` | histogram | Количество возвращённых строк |
| `queryguard_active_connections` | gauge | Текущее количество активных соединений |

---

## Деплой в Kubernetes

```bash
make docker-build
make k8s-deploy
```

Прокси доступен внутри кластера как `queryguard-proxy:5433`. Дашборд и метрики — через NodePort 30080 и 30090.

---

## Технологический стек

- Go 1.22
- [jackc/pgproto3](https://github.com/jackc/pgx) — парсер PostgreSQL Wire Protocol
- [pganalyze/pg_query_go](https://github.com/pganalyze/pg_query_go) — SQL AST через C-парсер PostgreSQL
- [prometheus/client_golang](https://github.com/prometheus/client_golang) — метрики
- [go.uber.org/zap](https://github.com/uber-go/zap) — структурированное логирование
- htmx + Server-Sent Events — веб-дашборд без JS-фреймворков
- gopkg.in/yaml.v3 — конфигурация политик

---

## Ограничения текущей версии

- TLS-соединения не поддерживаются: при попытке клиента установить SSL прокси отвечает `N` и продолжает работу без шифрования
- Connection pooling отсутствует: одно клиентское соединение создаёт одно соединение к postgres
- Статистика хранится в памяти: перезапуск прокси сбрасывает накопленные данные
- Extended Query Protocol поддерживается частично: `Parse / Bind / Execute` пересылаются прозрачно, но анализируется только SQL из `Parse`

---

## Лицензия

MIT

APP_NAME    = queryguard
BUILD_DIR   = ./bin
CONFIG_PATH = configs/config.yaml
IMAGE_NAME  = queryguard
IMAGE_TAG   = latest

## Запуск
run:
	-sudo fuser -k 5433/tcp 2>/dev/null; sleep 0.3
	go run ./cmd/queryguard/... -config $(CONFIG_PATH); true

## Сборка бинарника
build:
	CGO_ENABLED=1 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/queryguard/...

## Unit тесты
test:
	go test ./... -v -race -count=1

## Интеграционные тесты (требуют запущенного прокси и postgres)
test-integration:
	go test ./tests/integration/... -v -timeout=30s

## Покрытие
test-cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "coverage report: coverage.html"

## Docker образ
docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .
	@echo "Image size:"
	@docker images $(IMAGE_NAME):$(IMAGE_TAG) --format "{{.Size}}"

## Docker compose
docker-up:
	docker compose -f docker/docker-compose.yml up -d
	@echo "PostgreSQL:  localhost:5432"
	@echo "pgAdmin:     http://localhost:5050"
	@echo "Prometheus:  http://localhost:9091"
	@echo "Grafana:     http://localhost:3000"

docker-down:
	docker compose -f docker/docker-compose.yml down

docker-clean:
	docker compose -f docker/docker-compose.yml down -v

## K8s деплой (требует kubectl + кластер)
k8s-deploy:
	kubectl apply -f k8s/namespace.yaml
	kubectl apply -f k8s/secret.yaml
	kubectl apply -f k8s/serviceaccount.yaml
	kubectl apply -f k8s/configmap.yaml
	kubectl apply -f k8s/networkpolicy.yaml
	kubectl apply -f k8s/deployment.yaml
	kubectl apply -f k8s/service.yaml
	kubectl rollout status deployment/queryguard -n queryguard

k8s-delete:
	kubectl delete namespace queryguard

## psql соединения
psql-direct:
	psql -h localhost -p 5432 -U postgres postgres

psql-proxy:
	psql -h localhost -p 5433 -U postgres postgres

## Освободить порт
kill-proxy:
	-sudo fuser -k 5433/tcp 2>/dev/null
	@echo "port 5433 freed"

## Зависимости
tidy:
	go mod tidy

lint:
	golangci-lint run ./...

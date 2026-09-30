# AI Reply backend — жиі қолданылатын командалар.
BINARY := bin/server

.PHONY: run build test vet fmt tidy clean docker secrets

run: ## Серверді жергілікті іске қосу (.env қажет)
	go run ./cmd/server

build: ## Бинарник жинау
	CGO_ENABLED=1 go build -trimpath -o $(BINARY) ./cmd/server

test: ## Барлық тесттер
	go test ./... -count=1

test-race: ## Квота бәсекесін нақты тексеру
	go test ./... -count=1 -race

vet: ## Статикалық тексеру
	go vet ./...

fmt: ## Форматтау
	gofmt -w .

tidy: ## go.mod тазалау
	go mod tidy

secrets: ## Жаңа құпия кілттер генерациялау
	@echo "JWT_ACCESS_SECRET=$$(openssl rand -hex 32)"
	@echo "JWT_REFRESH_SECRET=$$(openssl rand -hex 32)"
	@echo "AUTH_SIGNING_SECRET=$$(openssl rand -hex 32)"

docker: ## Docker арқылы көтеру (барлық порт .env ішіндегі APP_PORT-тан)
	docker compose up -d --build

clean:
	rm -rf bin data

.PHONY: run generate test test-integration test-web test-web-live build vet lint up down migrate seed smoke

run:
	go run ./cmd/screener

generate:
	go generate ./...

test:
	go test -race -shuffle=on ./...

test-integration:
	go test -race -shuffle=on -tags integration ./...

test-web:
	npm --prefix web run check
	npm --prefix web test

test-web-live:
	npm --prefix web run test:live

build:
	mkdir -p bin
	go build -trimpath -o bin/screener ./cmd/screener
	go build -trimpath -o bin/migrate ./cmd/migrate

vet:
	go vet ./...

lint: vet
	golangci-lint run --build-tags integration

up:
	docker compose up --build -d

down:
	docker compose down

migrate:
	go run ./cmd/migrate up

seed:
	go run ./cmd/migrate seed

smoke:
	curl --fail --silent --show-error http://127.0.0.1:8080/healthz
	curl --fail --silent --show-error http://127.0.0.1:8080/readyz

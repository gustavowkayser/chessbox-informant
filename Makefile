.PHONY: db-up migrate build test test-books lint dev fixtures

export CHESSINFO_TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5433/postgres

ifeq ($(OS),Windows_NT)
EXE := .exe
endif

db-up:
	docker compose up -d --wait postgres

migrate:
	cd backend && go run . migrate

build:
	cd backend && go build -o bin/chessinfo$(EXE) .

test:
	cd backend && go test ./...

test-books:
	cd backend && go test ./tests/books -v -count=1

lint:
	cd backend && go vet ./...
	cd frontend && bun run lint

dev:
	cd frontend && bun dev

fixtures:
	cd backend && go run ./tests/fixturegen

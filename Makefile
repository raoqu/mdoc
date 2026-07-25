.PHONY: dev api build static
dev:
	pnpm run dev
api:
	go run ./cmd/mdocman
build:
	pnpm run build
static:
	curl -X POST http://localhost:8080/api/build

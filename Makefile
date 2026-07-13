.PHONY: dev api build static
dev:
	npm run dev
api:
	go run ./cmd/mdocman
build:
	npm run build
static:
	curl -X POST http://localhost:8080/api/build

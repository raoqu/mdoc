.PHONY: dev api build static
dev:
	pnpm run dev
api:
	go run -tags extended ./cmd/mdocman
build:
	pnpm run build
static:
	./publish.sh build

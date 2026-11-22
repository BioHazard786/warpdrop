.PHONY: all build-frontend build-backend build-cli build test test-e2e run dev-web dev-backend lint fmt clean

all: build

# Vite writes straight into backend/web/dist, which the Go binary embeds.
build-frontend:
	cd web && bun install --frozen-lockfile && bun run build

build-backend:
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o server ./cmd/server

build-cli:
	cd cli && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/warpdrop .

build: build-frontend build-backend

test:
	cd backend && go test -race -count=1 ./...
	cd cli && go test -race -count=1 ./...
	cd web && bun run test

test-e2e:
	cd e2e && bun install --frozen-lockfile && bun x playwright test

run: build
	./backend/server

# Vite dev server on :5173; run dev-backend alongside it.
dev-web:
	cd web && bun run dev

dev-backend:
	cd backend && go run ./cmd/server

lint:
	cd web && bun run lint && bun run fmt:check && bun run typecheck

fmt:
	cd backend && gofmt -w .
	cd cli && gofmt -w .
	cd web && bun run fmt

clean:
	rm -rf backend/server cli/dist
	find backend/web/dist -mindepth 1 ! -name .gitkeep -delete

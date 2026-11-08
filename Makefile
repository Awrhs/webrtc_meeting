.PHONY: build-backend build-frontend build-all run run-dev docker-up docker-down clean e2e-up e2e-backend e2e-full

# ---- 完整 E2E（本地）----
# 1) make e2e-up      启动 Postgres/Redis 容器
# 2) make e2e-backend 在前台启动 SFU + API（另开终端）
# 3) make e2e-full    构建前端并用 Chromium 跑完整会议用例
e2e-up:
	docker compose -f deployment/docker-compose.yml -f deployment/docker-compose.dev.yml up -d postgres redis
	@echo "Postgres: localhost:5433  Redis: localhost:6379 — 现在运行 make e2e-backend"

e2e-backend:
	@echo "Starting SFU + API against local containers (Ctrl-C stops both)..."
	GO_ENV=development DB_PORT=5433 RATE_LIMIT_AUTH_PER_MINUTE=100 ALLOWED_ORIGINS=http://localhost:3000,http://127.0.0.1:3000 \
		sh -c 'go run ./backend/cmd/sfu & SFU_PID=$$!; trap "kill $$SFU_PID" EXIT; go run ./backend/cmd/server'

e2e-full:
	cd frontend && VITE_API_HOST=http://localhost:8080 VITE_SFU_HOST=http://localhost:8081 npm run build && \
		E2E_FULL=1 npx playwright test --project=chromium

build-backend:
	@echo "Building backend..."
	cd backend/cmd/server && go build -o ../../../bin/server
	cd backend/cmd/sfu && go build -o ../../../bin/sfu

build-frontend:
	@echo "Building frontend..."
	cd frontend && npm install && npm run build

build-all: build-backend build-frontend

run: build-all
	@echo "Starting API server..."
	./bin/server &
	@echo "Starting SFU server..."
	./bin/sfu &

run-dev:
	@echo "Starting API server in development mode..."
	air -c .air.toml &
	@echo "Starting SFU server..."
	go run backend/cmd/sfu/main.go &
	@echo "Starting frontend dev server..."
	cd frontend && npm run dev &

docker-up:
	docker-compose -f deployment/docker-compose.yml up -d --build

docker-down:
	docker-compose -f deployment/docker-compose.yml down

docker-logs:
	docker-compose -f deployment/docker-compose.yml logs -f

clean:
	rm -rf bin/
	rm -rf frontend/dist/

.PHONY: build-backend build-frontend build-all run run-dev docker-up docker-down clean

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

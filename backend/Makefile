.PHONY: proto
proto:
	@echo "Generating gRPC code..."
	protoc --go_out=api/gen/go --go_opt=paths=source_relative \
		--go-grpc_out=api/gen/go --go-grpc_opt=paths=source_relative \
		api/proto/sfu_control.proto
	@echo "Done!"

.PHONY: install-tools
install-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

.PHONY: run-server
run-server:
	cd cmd/server && go run main.go

.PHONY: run-sfu
run-sfu:
	cd cmd/sfu && go run main.go

.PHONY: docker-up
docker-up:
	docker-compose -f ../deployment/docker-compose.yml up -d

.PHONY: docker-down
docker-down:
	docker-compose -f ../deployment/docker-compose.yml down

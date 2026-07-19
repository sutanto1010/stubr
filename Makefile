.PHONY: build run test lint fmt tidy install docker-build docker-run docker-push clean

APP_NAME   := stubr
BIN_DIR    := bin
IMAGE_NAME := stubr
VERSION    ?= latest

build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN_DIR)/$(APP_NAME) ./cmd/$(APP_NAME)

run:
	go run ./cmd/$(APP_NAME)

test:
	go test -v -race -count=1 ./...

lint:
	go vet ./...

fmt:
	go fmt ./...

tidy:
	go mod tidy

install: build
	cp $(BIN_DIR)/$(APP_NAME) /usr/local/bin/$(APP_NAME)

docker-build:
	docker build -t $(IMAGE_NAME):$(VERSION) .

docker-run:
	docker run --rm -p 8080:8080 -v $(PWD)/stubs:/stubs -v $(PWD)/stubr.yaml:/etc/stubr.yaml $(IMAGE_NAME):$(VERSION) -config /etc/stubr.yaml -dir /stubs

docker-push:
	docker push $(IMAGE_NAME):$(VERSION)

clean:
	rm -rf $(BIN_DIR)
	go clean -cache

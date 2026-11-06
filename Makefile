IMAGE_NAME := lavery98/zen-maintenance-exporter
BINARY := zen-maintenance-exporter
GOARCH ?= $(shell go env GOARCH)

.PHONY: build
build:
	CGO_ENABLED=0 go build -a --trimpath --installsuffix cgo --ldflags '-s' -o $(BINARY)

.PHONY: test
test:
	go test ./...

.PHONY: image
image:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build --trimpath --ldflags '-s' -o linux/$(GOARCH)/$(BINARY)
	docker build --platform linux/$(GOARCH) -t ${IMAGE_NAME} .

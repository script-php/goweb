APP_NAME ?= website
RELEASE_DIR ?= release
VERSION ?= dev

.PHONY: build clean test

build:
	mkdir -p "$(RELEASE_DIR)"
	@if [ ! -f "$(RELEASE_DIR)/config.json" ]; then cp config.example.json "$(RELEASE_DIR)/config.json"; fi
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o "$(RELEASE_DIR)/$(APP_NAME)" .

test:
	go test ./...

clean:
	rm -f "$(RELEASE_DIR)/$(APP_NAME)" "$(RELEASE_DIR)/tables.hash"


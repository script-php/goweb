APP_NAME ?= website
RELEASE_DIR ?= release

.PHONY: backup build check clean integration-test release-check restore run test verify-backup

build:
	mkdir -p "$(RELEASE_DIR)"
	mkdir -p "$(RELEASE_DIR)/uploads"
	@test -s version.txt || { echo "version.txt must contain a version"; exit 1; }
	@if [ ! -f "$(RELEASE_DIR)/config.json" ]; then cp config.example.json "$(RELEASE_DIR)/config.json"; fi
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$(RELEASE_DIR)/$(APP_NAME)" .

test:
	go test ./...

check:
	go test -race ./...
	go vet ./...

release-check: check
	./scripts/check-release.sh

integration-test:
	@test -n "$$WEBSITE_INTEGRATION_DSN" || { echo "WEBSITE_INTEGRATION_DSN is required"; exit 1; }
	@test "$$WEBSITE_INTEGRATION_ALLOW_RESTORE" = "YES" || { echo "Set WEBSITE_INTEGRATION_ALLOW_RESTORE=YES and use only a disposable database"; exit 1; }
	go test -p 1 -tags=integration ./... -run Integration -count=1

run: build
	cd "$(RELEASE_DIR)" && ./"$(APP_NAME)"

backup: build
	cd "$(RELEASE_DIR)" && ./"$(APP_NAME)" --backup backups

verify-backup: build
	@test -n "$(BACKUP)" || { echo "Use: make verify-backup BACKUP=/path/to/backup.tar.gz"; exit 1; }
	cd "$(RELEASE_DIR)" && ./"$(APP_NAME)" --verify-backup "$(abspath $(BACKUP))"

restore: build
	@test "$(CONFIRM)" = "YES" || { echo "Restore replaces database data and uploads. Re-run with CONFIRM=YES"; exit 1; }
	@test -n "$(BACKUP)" || { echo "Use: make restore BACKUP=/path/to/backup.tar.gz CONFIRM=YES"; exit 1; }
	cd "$(RELEASE_DIR)" && ./"$(APP_NAME)" --restore "$(abspath $(BACKUP))" --confirm-restore

clean:
	rm -f "$(RELEASE_DIR)/$(APP_NAME)" "$(RELEASE_DIR)/tables.hash"

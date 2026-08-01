.DEFAULT_GOAL := install

GO ?= go
INSTALL ?= install
OPEN ?= open
CURL ?= curl
BREW ?= brew
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
BUILD_DIR ?= bin
SERVER_DIR ?= $(abspath ../local-web-app-server)
APPS_DIR ?= $(HOME)/Library/Application Support/LocalWebAppServer/apps
APP_DIR ?= $(APPS_DIR)/chapter-brake
SERVER_URL ?= http://127.0.0.1:8766
SERVER_LISTEN ?= 0.0.0.0:8766

BINARY := chapterbrake
PACKAGE := ./cmd/chapterbrake
SERVER_BINARY := $(BINDIR)/local-web-app-server
APP_URL := $(SERVER_URL)/apps/chapter-brake/
WEB_FILES := web/index.html web/styles.css web/app.js web/model.mjs web/favicon.svg
WEB_VENDOR_FILES := web/vendor/sortable.min.js web/vendor/SORTABLE_LICENSE.txt

.PHONY: all build install dependencies doctor install-server install-app start run open stop test check clean

all: install

build:
	mkdir -p "$(BUILD_DIR)"
	$(GO) build -trimpath -o "$(BUILD_DIR)/$(BINARY)" $(PACKAGE)

install: dependencies install-server install-app start

dependencies:
	@if ! command -v HandBrakeCLI >/dev/null 2>&1; then \
		command -v "$(BREW)" >/dev/null 2>&1 || { echo "Homebrew is required to install HandBrakeCLI" >&2; exit 1; }; \
		"$(BREW)" install --cask handbrake; \
	fi
	@if ! command -v ffmpeg >/dev/null 2>&1 || ! command -v ffprobe >/dev/null 2>&1; then \
		command -v "$(BREW)" >/dev/null 2>&1 || { echo "Homebrew is required to install FFmpeg" >&2; exit 1; }; \
		"$(BREW)" install ffmpeg; \
	fi
	@if ! command -v mkvpropedit >/dev/null 2>&1; then \
		command -v "$(BREW)" >/dev/null 2>&1 || { echo "Homebrew is required to install MKVToolNix" >&2; exit 1; }; \
		"$(BREW)" install mkvtoolnix; \
	fi

doctor:
	@command -v HandBrakeCLI
	@command -v ffmpeg
	@command -v ffprobe
	@command -v mkvpropedit
	@HandBrakeCLI --version
	@ffmpeg -version | head -n 1
	@ffprobe -version | head -n 1
	@mkvpropedit --version
	@test -x "$(SERVER_BINARY)"
	@"$(CURL)" -fsS "$(APP_URL)api/status"

install-server:
	$(MAKE) -C "$(SERVER_DIR)" install PREFIX="$(PREFIX)"

install-app: build
	$(INSTALL) -d "$(APP_DIR)/bin" "$(APP_DIR)/web" "$(APP_DIR)/web/vendor"
	$(INSTALL) -m 0755 "$(BUILD_DIR)/$(BINARY)" "$(APP_DIR)/bin/$(BINARY)"
	$(INSTALL) -m 0644 local-web-app.json "$(APP_DIR)/local-web-app.json"
	$(INSTALL) -m 0644 $(WEB_FILES) "$(APP_DIR)/web"
	$(INSTALL) -m 0644 $(WEB_VENDOR_FILES) "$(APP_DIR)/web/vendor"

start:
	@"$(SERVER_BINARY)" --stop
	@nohup "$(SERVER_BINARY)" --listen "$(SERVER_LISTEN)" >/dev/null 2>&1 &
	@attempt=0; \
	while ! "$(CURL)" -fsS "$(APP_URL)api/status" >/dev/null 2>&1; do \
		attempt=$$((attempt + 1)); \
		if [ $$attempt -ge 100 ]; then \
			echo "ChapterBrake Web backend did not become ready: $(APP_URL)" >&2; \
			exit 1; \
		fi; \
		sleep 0.1; \
	done
	@$(OPEN) "$(APP_URL)"
	@echo "ChapterBrake: $(APP_URL)"

run: start

open:
	@$(OPEN) "$(APP_URL)"

stop:
	@"$(SERVER_BINARY)" --stop

test:
	$(GO) test ./...
	node --test web/model.test.mjs

check:
	gofmt -w .
	$(GO) test ./...
	$(GO) test -race ./...
	$(GO) vet ./...
	$(GO) build ./...
	node --test web/model.test.mjs
	node --check web/app.js

clean:
	rm -rf "$(BUILD_DIR)"

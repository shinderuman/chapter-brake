.DEFAULT_GOAL := install

GO ?= go
INSTALL ?= install
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
BUILD_DIR ?= bin
BINARY := chapterbrake
PACKAGE := ./cmd/chapterbrake

.PHONY: build install test

build:
	mkdir -p "$(BUILD_DIR)"
	$(GO) build -trimpath -o "$(BUILD_DIR)/$(BINARY)" $(PACKAGE)

install: build
	mkdir -p "$(BINDIR)"
	$(INSTALL) -m 0755 "$(BUILD_DIR)/$(BINARY)" "$(BINDIR)/$(BINARY)"

test:
	$(GO) test ./...

GO ?= go
ROOT := $(CURDIR)
RUNTIME := $(ROOT)/runtime
APP := $(RUNTIME)/apps/chapter-brake

.PHONY: build install test check clean

build:
	mkdir -p "$(RUNTIME)" "$(APP)/bin" "$(APP)/web"
	cd generic-server && $(GO) build -o "$(RUNTIME)/local-web-server" ./cmd/local-web-server
	cd chapter-brake && $(GO) build -o "$(APP)/bin/chapterbrake-poc-backend" ./cmd/chapterbrake-poc-backend

install: build
	cp chapter-brake/app.json "$(APP)/app.json"
	cp chapter-brake/web/index.html chapter-brake/web/app.css chapter-brake/web/app.js chapter-brake/web/'日本語 空白 #記号.txt' "$(APP)/web/"

test:
	cd generic-server && $(GO) test ./...
	cd chapter-brake && $(GO) test ./...

check: test
	@test -z "$$(gofmt -l generic-server chapter-brake)"
	cd generic-server && $(GO) vet ./...
	cd chapter-brake && $(GO) vet ./...

clean:
	rm -f "$(RUNTIME)/local-web-server"
	rm -f "$(APP)/app.json" "$(APP)/bin/chapterbrake-poc-backend"
	rm -f "$(APP)/web/index.html" "$(APP)/web/app.css" "$(APP)/web/app.js" "$(APP)/web/日本語 空白 #記号.txt"

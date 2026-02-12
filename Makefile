GO ?= /usr/local/go/bin/go
BINARY := simpleagent
SHELL := /bin/bash

.PHONY: build install clean vet tidy bump release all

build:
	$(GO) build -ldflags "-X main.version=$$(cat VERSION)" -o $(BINARY) .

install:
	$(GO) install -ldflags "-X main.version=$$(cat VERSION)" .

clean:
	rm -f $(BINARY)

vet:
	$(GO) vet .

tidy:
	$(GO) mod tidy

# Auto-generate next version: yymmddvv
# If today matches, increment vv. Otherwise reset to 01.
bump:
	@today=$$(date +%y%m%d); \
	current=$$(cat VERSION 2>/dev/null || echo ""); \
	prefix=$${current:0:6}; \
	if [ "$$prefix" = "$$today" ]; then \
		counter=$${current:6:2}; \
		next=$$(printf "%02d" $$((10#$$counter + 1))); \
	else \
		next="01"; \
	fi; \
	echo "$${today}$${next}" > VERSION; \
	echo "Version: $$(cat VERSION)"

# Local: bump + build + install
release: bump build install
	@echo "Released: $$(cat VERSION)"

# Remote: just push to main — GitHub Actions handles the rest
# (auto bumps version, builds binaries, creates GitHub release, updates CHANGELOG.md)

all: tidy vet build

.PHONY: build test vet release
VERSION ?= dev

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/locmonitor ./cmd/locmonitor

test:
	go test ./...

vet:
	go vet ./...

release:
	./scripts/release.sh $(VERSION)

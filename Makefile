BINARY := gitshelf
PKG := ./cmd/gitshelf
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test vet run clean docker fmt

all: vet test build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

run: build
	./$(BINARY) -config gitshelf.toml

docker:
	docker build -t $(BINARY):$(VERSION) .

clean:
	rm -f $(BINARY)
	rm -rf dist/

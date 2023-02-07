DATE    = $(shell date +%Y%m%d%H%M)
IMAGE   ?= keppel.eu-de-1.cloud.sap/ccloud/vpa_butler
VERSION ?= v0.0.6
GOOS    ?= $(shell go env | grep GOOS | cut -d'"' -f2)
BINARY  := vpa_butler
OPTS    ?=

SRCDIRS  := cmd internal
PACKAGES := $(shell find $(SRCDIRS) -type d)
GO_PKG	 := github.com/sapcc/vpa_butler
GOFILES  := $(addsuffix /*.go,$(PACKAGES))
GOFILES  := $(wildcard $(GOFILES))

.PHONY: all clean vendor tests static-check

all: bin/$(GOOS)/$(BINARY)

bin/%/$(BINARY): GIT_COMMIT  = $(shell git rev-parse --short HEAD)
bin/%/$(BINARY): BUILD_DATE  = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
bin/%/$(BINARY): $(GOFILES) Makefile
	CGO_ENABLED=0 GOOS=$* GOARCH=amd64 go build -ldflags '-X main.Version=$(VERSION)' -mod vendor -v -o bin/$*/$(BINARY) ./cmd/vpa_butler/main.go && chmod +x bin/$*/$(BINARY)

build:
	docker build $(OPTS) -t $(IMAGE):$(VERSION) .

push: build
	docker push $(IMAGE):$(VERSION)

clean:
	rm -rf bin/*

vendor:
	go mod tidy && go mod vendor

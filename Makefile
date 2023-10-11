DATE    = $(shell date +%Y%m%d%H%M)
IMAGE   ?= keppel.eu-de-1.cloud.sap/ccloud/vpa_butler
VERSION ?= $(shell git describe --tags --always --dirty)
GOOS    ?= $(shell go env | grep GOOS | cut -d"'" -f2)
BINARY  := vpa_butler
OPTS    ?=

SRCDIRS  := cmd internal
PACKAGES := $(shell find $(SRCDIRS) -type d)
GO_PKG	 := github.com/sapcc/vpa_butler
GOFILES  := $(addsuffix /*.go,$(PACKAGES))
GOFILES  := $(wildcard $(GOFILES))
GOPATH = $(shell go env GOPATH)

.PHONY: all clean test

all: bin/$(GOOS)/$(BINARY)

bin/%/$(BINARY): GIT_COMMIT  = $(shell git rev-parse --short HEAD)
bin/%/$(BINARY): BUILD_DATE  = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
bin/%/$(BINARY): $(GOFILES) Makefile
	CGO_ENABLED=0 GOOS=$* GOARCH=amd64 go build -ldflags '-X main.Version=$(VERSION)' -v -o bin/$*/$(BINARY) ./cmd/vpa_butler/main.go && chmod +x bin/$*/$(BINARY)

lint:
	golangci-lint run --timeout=5m

test: lint
	bash -c "source <($(GOPATH)/bin/setup-envtest use -p env); $(GOPATH)/bin/ginkgo --randomize-all ./..."

cover: lint
	bash -c "source <($(GOPATH)/bin/setup-envtest use -p env); $(GOPATH)/bin/ginkgo --randomize-all --coverpkg=github.com/sapcc/vpa_butler/... ./..."
	go tool cover -html=coverprofile.out

build:
	docker build $(OPTS) -t $(IMAGE):$(VERSION) .

push: build
	docker push $(IMAGE):$(VERSION)

clean:
	rm -rf bin/*

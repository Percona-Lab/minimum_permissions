.PHONY: all style format build test vet tarball linux-amd64 help prepare init clean
default: help

GO := go
pkgs   = $(shell basename `git rev-parse --show-toplevel`)
VERSION ?=$(shell git describe --abbrev=0)
BUILD ?=$(shell date +%FT%T%z)
GOVERSION ?=$(shell go version | cut --delimiter=" " -f3)
COMMIT ?=$(shell git rev-parse HEAD)
BRANCH ?=$(shell git rev-parse --abbrev-ref HEAD)
GOPATH ?=${HOME}/go

MAKE_TARS = ''
CUR_DIR=$(shell pwd)
BIN_DIR=${CUR_DIR}/build
LDFLAGS="-X main.Version=${VERSION} -X main.Build=${BUILD} -X main.Commit=${COMMIT} -X main.Branch=${BRANCH} -X main.GoVersion=${GOVERSION} -s -w"

FILES = $(shell find . -type f -name '*.go' -not -path "./vendor/*")

all: clean darwin-amd64-tar linux-amd64-tar 

init:                       	## Install linters.
	go build -modfile=tools/go.mod -o bin/gofumports mvdan.cc/gofumpt/gofumports
	go build -modfile=tools/go.mod -o bin/gofumpt mvdan.cc/gofumpt
	go build -modfile=tools/go.mod -o bin/golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint

build:
	@$(info Cleaning old tar files in ${BIN_DIR})
	@rm -f ${BIN_DIR}/minimum_permissions_*.tar.gz
	@echo
	@$(info Building in ${BIN_DIR})
	@go build -ldflags ${LDFLAGS} -o ${BIN_DIR}/minimum_permissions main.go

help:							## Display this help message.
	@echo "Please use \`make <target>\` where <target> is one of:"
	@grep '^[a-zA-Z]' $(MAKEFILE_LIST) | \
	awk -F ':.*?## ' 'NF==2 {printf "  %-26s%s\n", $$1, $$2}'

prepare: 
	@$(info Checking if ${BIN_DIR} exists)
	@mkdir -p ${BIN_DIR}

clean: prepare					## Clean old builds leftovers
	@$(info Cleaning binaries and tar.gz files in dir ${BIN_DIR})
	@rm -f ${BIN_DIR}/minimum_permissions
	@rm -f ${BIN_DIR}/minimum_permissions_*.tar.gz

linux-amd64: prepare			## Build linux-amd64 binary.
	@echo "Building linux/amd64 binaries in ${BIN_DIR}"
	@GOOS=linux GOARCH=amd64 go build -ldflags ${LDFLAGS} -o ${BIN_DIR}/minimum_permissions main.go

linux-amd64-tar: linux-amd64    ## Build linux-amd64 binary and compress it.
	@tar cvzf ${BIN_DIR}/minimum_permissions_linux_amd64.tar.gz -C ${BIN_DIR} minimum_permissions

darwin-amd64: prepare 			## Build darwin binary. 
	@echo "Building darwin/amd64 binaries in ${BIN_DIR}"
	@mkdir -p ${BIN_DIR}
	@GOOS=darwin GOARCH=amd64 go build -ldflags ${LDFLAGS} -o ${BIN_DIR}/minimum_permissions main.go

darwin-amd64-tar: darwin-amd64	## Build darwin binary and compress it.
	@tar cvzf ${BIN_DIR}/minimum_permissions_darwin_amd64.tar.gz -C ${BIN_DIR} minimum_permissions

style:							## Check code style.
	@echo ">> checking code style"
	@! gofmt -d $(shell find . -path ./vendor -prune -o -name '*.go' -print) | grep '^'

test:							## Run tests.
	@echo ">> running tests"
	@./runtests.sh

env-down:						## Stop docker testing container.
	@$(info Cleaning up docker containers used for tests)
	@docker-compose down

format:
	@echo ">> formatting code"
	bin/gofumports -local github.com/Percona-Lab/minimum_permissions -l -w $(FILES)
	bin/gofumpt -w -s $(FILES)

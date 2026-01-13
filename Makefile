export PATH := $(PATH):`go env GOPATH`/bin
export GO111MODULE=on
LDFLAGS := -s -w
NOWEB_TAG = $(shell [ ! -d web/frps/dist ] || [ ! -d web/frpc/dist ] && echo ',noweb')

.PHONY: web frps-web frpc-web frps qemu

all: env fmt web build

build: frps qemu

env:
	@go version

web: frps-web frpc-web

frps-web:
	$(MAKE) -C web/frps build

frpc-web:
	$(MAKE) -C web/frpc build

fmt:
	go fmt ./...

fmt-more:
	gofumpt -l -w .

gci:
	gci write -s standard -s default -s "prefix(github.com/fatedier/frp/)" ./

vet:
	go vet -tags "$(NOWEB_TAG)" ./...

frps:
	env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -tags "frps$(NOWEB_TAG)" -o bin/frps ./cmd/frps

qemu:
	env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -tags "frpc$(NOWEB_TAG)" -o bin/qemu ./cmd/frpc

test: gotest

gotest:
	go test -tags "$(NOWEB_TAG)" -v --cover ./assets/...
	go test -tags "$(NOWEB_TAG)" -v --cover ./cmd/...
	go test -tags "$(NOWEB_TAG)" -v --cover ./client/...
	go test -tags "$(NOWEB_TAG)" -v --cover ./server/...
	go test -tags "$(NOWEB_TAG)" -v --cover ./pkg/...

e2e:
	./hack/run-e2e.sh

e2e-trace:
	DEBUG=true LOG_LEVEL=trace ./hack/run-e2e.sh

e2e-compatibility-last-qemu:
	if [ ! -d "./lastversion" ]; then \
		TARGET_DIRNAME=lastversion ./hack/download.sh; \
	fi
	FRPC_PATH="`pwd`/lastversion/qemu" ./hack/run-e2e.sh
	rm -r ./lastversion

e2e-compatibility-last-frps:
	if [ ! -d "./lastversion" ]; then \
		TARGET_DIRNAME=lastversion ./hack/download.sh; \
	fi
	FRPS_PATH="`pwd`/lastversion/frps" ./hack/run-e2e.sh
	rm -r ./lastversion

alltest: vet gotest e2e
	
clean:
	rm -f ./bin/qemu
	rm -f ./bin/frps
	rm -rf ./lastversion

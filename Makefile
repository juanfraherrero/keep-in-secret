.PHONY: build test test-e2e test-update test-docker fuzz install

build:
	go build -o bin/kis ./cmd/kis
	ln -sf kis bin/git-kis

test:
	go vet ./...
	go test ./...

test-e2e:
	go test ./cmd/kis -run TestScripts -v

test-update:
	go test ./cmd/kis -run TestScripts -update

# Needs a running Docker engine (e.g. `colima start`). TARGETS="git:2.30.2 alpine:3.20" to narrow.
test-docker:
	test/docker/run.sh $(TARGETS)

FUZZTIME ?= 30s
fuzz:
	go test ./internal/crypt -run '^$$' -fuzz FuzzDecrypt -fuzztime $(FUZZTIME)
	go test ./internal/crypt -run '^$$' -fuzz FuzzRoundTrip -fuzztime $(FUZZTIME)
	go test ./internal/filter -run '^$$' -fuzz FuzzProcess -fuzztime $(FUZZTIME)

install:
	go install ./cmd/kis
	ln -sf "$$(go env GOPATH)/bin/kis" "$$(go env GOPATH)/bin/git-kis"

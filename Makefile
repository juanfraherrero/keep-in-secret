.PHONY: build test test-e2e test-update fuzz install

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

FUZZTIME ?= 30s
fuzz:
	go test ./internal/crypt -run '^$$' -fuzz FuzzDecrypt -fuzztime $(FUZZTIME)
	go test ./internal/crypt -run '^$$' -fuzz FuzzRoundTrip -fuzztime $(FUZZTIME)
	go test ./internal/filter -run '^$$' -fuzz FuzzProcess -fuzztime $(FUZZTIME)

install:
	go install ./cmd/kis
	ln -sf "$$(go env GOPATH)/bin/kis" "$$(go env GOPATH)/bin/git-kis"

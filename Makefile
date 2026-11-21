.PHONY: build test test-e2e test-update install

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

install:
	go install ./cmd/kis
	ln -sf "$$(go env GOPATH)/bin/kis" "$$(go env GOPATH)/bin/git-kis"

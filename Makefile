.PHONY: build test install

build:
	go build -o bin/kis ./cmd/kis
	ln -sf kis bin/git-kis

test:
	go vet ./...
	go test ./...

install:
	go install ./cmd/kis
	ln -sf "$$(go env GOPATH)/bin/kis" "$$(go env GOPATH)/bin/git-kis"

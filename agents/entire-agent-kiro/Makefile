BINARY := entire-agent-kiro

.PHONY: build test clean

build:
	GOCACHE=/tmp/go-build-cache go build -o $(BINARY) ./cmd/entire-agent-kiro

test:
	GOCACHE=/tmp/go-build-cache go test ./...

clean:
	rm -f $(BINARY)

BINARY := entire-agent-mistral-vibe

.PHONY: build test clean

build:
	GOCACHE=/tmp/go-build-cache go build -o $(BINARY) ./cmd/entire-agent-mistral-vibe

test:
	GOCACHE=/tmp/go-build-cache go test ./...

clean:
	rm -f $(BINARY)

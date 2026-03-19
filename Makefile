.PHONY: test-e2e test-unit test-all

test-e2e:
	cd e2e && go test -tags=e2e -v -count=1 ./...

test-unit:
	@for dir in agents/entire-agent-*/; do \
		echo "Testing $$dir..."; \
		cd $$dir && go test ./... && cd ../..; \
	done

test-e2e-lifecycle:
	cd e2e && E2E_REQUIRE_LIFECYCLE=1 go test -tags=e2e -v -count=1 -run TestLifecycle ./...

test-all: test-unit test-e2e

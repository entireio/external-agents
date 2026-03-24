.PHONY: test-e2e test-unit test-all test-e2e-lifecycle

test-e2e:
	@$(MAKE) test-e2e-lifecycle AGENT="$(AGENT)"

test-e2e-lifecycle:
ifdef AGENT
	cd e2e && E2E_AGENT=$(AGENT) go test -tags=e2e -v -count=1 -run TestLifecycle ./...
else
	cd e2e && go test -tags=e2e -v -count=1 -run TestLifecycle ./...
endif

test-unit:
	@for dir in agents/entire-agent-*/; do \
		echo "Testing $$dir..."; \
		cd $$dir && go test ./... && cd ../..; \
	done

test-all: test-unit test-e2e-lifecycle

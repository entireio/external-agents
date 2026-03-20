.PHONY: test-e2e test-unit test-all test-e2e-lifecycle test-e2e-binary

test-e2e:
ifdef AGENT
	cd e2e && E2E_AGENT=$(AGENT) go test -tags=e2e -v -count=1 ./...
else
	cd e2e && go test -tags=e2e -v -count=1 ./...
endif

test-unit:
	@for dir in agents/entire-agent-*/; do \
		echo "Testing $$dir..."; \
		cd $$dir && go test ./... && cd ../..; \
	done

test-e2e-lifecycle:
ifdef AGENT
	cd e2e && E2E_AGENT=$(AGENT) E2E_REQUIRE_LIFECYCLE=1 go test -tags=e2e -v -count=1 -run TestLifecycle ./...
else
	cd e2e && E2E_REQUIRE_LIFECYCLE=1 go test -tags=e2e -v -count=1 -run TestLifecycle ./...
endif

test-e2e-binary:
ifdef AGENT
	cd e2e && go test -tags=e2e -v -count=1 ./$(AGENT)/
else
	@echo "Usage: make test-e2e-binary AGENT=kiro"
	@exit 1
endif

test-all: test-unit test-e2e

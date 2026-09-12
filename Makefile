# Local quality gates — GO_CLIENT_BOOTSTRAP §4. Race is required.
# Pattern A (adapters/jobsma) needs sibling ../koten_multi_agent_golang.
# When the sibling is present, tests run with -tags patterna.
.PHONY: help test vet race fmt ci conformance

GO_TAGS :=
ifeq ($(shell test -f ../koten_multi_agent_golang/go.mod && echo yes),yes)
GO_TAGS := -tags patterna
endif

help:
	@echo "Targets: test vet race fmt ci conformance"
	@echo "Pattern A tags: $(GO_TAGS)"

test:
	go test $(GO_TAGS) ./...

vet:
	go vet $(GO_TAGS) ./...

race:
	go test -race $(GO_TAGS) ./...

fmt:
	gofmt -w .

# Offline P-Suite adapter. Sibling zeus_client_design is required (hard fail).
# go test ./conformance skips the live suite when the design repo is absent
# so GitHub Actions (this repo only) stays green.
conformance:
	go run ./cmd/conformance

ci: fmt vet race test

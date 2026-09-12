# Local quality gates — GO_CLIENT_BOOTSTRAP §4. Race is required.
.PHONY: help test vet race fmt ci conformance

help:
	@echo "Targets: test vet race fmt ci conformance"

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

fmt:
	gofmt -w .

# Offline P-Suite adapter. Sibling zeus_client_design is required (hard fail).
# go test ./conformance skips the live suite when the design repo is absent
# so GitHub Actions (this repo only) stays green.
conformance:
	go run ./cmd/conformance

ci: fmt vet race test

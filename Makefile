# Local quality gates — GO_CLIENT_BOOTSTRAP §4. Race is required.
.PHONY: help test vet race fmt ci

help:
	@echo "Targets: test vet race fmt ci"

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

fmt:
	gofmt -w .

ci: fmt vet race test

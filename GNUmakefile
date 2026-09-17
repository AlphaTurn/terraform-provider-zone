BINARY := terraform-provider-zone

default: build

build:
	go build -o $(BINARY)

fmt:
	gofmt -w .
	terraform fmt -recursive ./examples/

lint:
	go vet ./...
	gofmt -l . | tee /dev/stderr | (! read)

# Runs unit tests and the stub-backed acceptance suite. Needs a terraform
# binary on PATH; touches no real DNS and needs no credentials.
test:
	go test ./... -timeout 20m

docs:
	tfplugindocs generate --provider-name zone

# Regenerates docs and fails if anything changed, so CI catches stale docs.
docs-check: docs
	git diff --exit-code -- docs/ || \
		(echo "docs/ is out of date — run 'make docs' and commit the result" && exit 1)

.PHONY: default build fmt lint test docs docs-check

# Development commands for argocd-ecr-updater

# Disable go.work (parent workspace interferes with standalone module builds)
export GOWORK := "off"

# Format all Go files (gofmt + goimports via golangci-lint)
fmt:
    golangci-lint fmt ./...

# Build all binaries
build: fmt
    go build -o bin/ecr-updater ./cmd/updater/

# Run unit tests
test:
    go test ./... -coverprofile=coverage.out


# Run linters. `config verify` first: the v2 schema silently accepts a
# stale settings block that only verify rejects.
lint:
    golangci-lint config verify
    golangci-lint run ./...

# The reason this repository can be public. Runs in CI as its own job.
leak-canary:
    hack/leak-canary.sh

# Run the tests under the race detector. Not part of `check`, and
# deliberately: everything else here builds with cgo off, which is what
# makes the binary static, and the race detector is the one thing that
# needs a C toolchain. CI runs this as its own job, where the toolchain
# is the runner's own.
race:
    CGO_ENABLED=1 go test -race ./...

# Run Go vulnerability check
vuln:
    govulncheck ./...

# Run go mod tidy
tidy:
    go mod tidy

# Clean build artifacts
clean:
    rm -rf bin/ dist/ coverage.out

# Run all checks (build + unit tests + integration tests + lint + vuln)
# Render the chart with representative values; prove the schema rejects
# an unknown key (values.schema.json is the contract — a typo must fail
# the render, not be silently ignored).
chart-lint:
    helm lint charts/argocd-ecr-updater
    helm template argocd-ecr-updater charts/argocd-ecr-updater \
        --set image.tag=0.0.0 \
        --set 'registries[0].secret=ecr-repo-creds-example' \
        --set 'registries[0].url=oci://<account>.dkr.ecr.<region>.amazonaws.com' >/dev/null
    ! helm template argocd-ecr-updater charts/argocd-ecr-updater --set bogusKey=1 >/dev/null 2>&1

check: build test lint chart-lint leak-canary vuln

# Build a snapshot release locally (no push, no tag)
snapshot:
    goreleaser release --snapshot --clean

# Package Helm chart locally
helm-package:
    helm package charts/argocd-ecr-updater --destination dist/

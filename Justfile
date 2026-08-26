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


# Run linters
lint:
    golangci-lint run ./...

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
        --set keysSecretName=example-keys >/dev/null
    ! helm template argocd-ecr-updater charts/argocd-ecr-updater --set bogusKey=1 >/dev/null 2>&1

check: build test lint chart-lint vuln

# Build a snapshot release locally (no push, no tag)
snapshot:
    goreleaser release --snapshot --clean

# Package Helm chart locally
helm-package:
    helm package charts/argocd-ecr-updater --destination dist/

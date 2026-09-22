# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it privately via
[GitHub Security Advisories](https://github.com/truvity/argocd-ecr-updater/security/advisories/new).

Do NOT open a public issue for security vulnerabilities.

## Supported Versions

Only the latest release is supported with security updates.

## Scope

The component's job is to hold a registry credential for a short time
and write it into a Kubernetes Secret. Reports about the token's
handling (where it can appear, what the ServiceAccount can reach, what
the render grants) are the ones that matter most; [docs/safety.md](docs/safety.md)
describes the intended boundaries.

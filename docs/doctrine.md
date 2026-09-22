# Doctrine — the design rules

## Refresh, do not proxy

An ECR authorization token lives twelve hours and Argo CD has no native
refresher. The two ways to close that gap are to put something between
Argo CD and the registry that authenticates on its behalf, or to keep
the credential Argo CD already reads fresh. This repository does the
second: it writes into the repo-creds Secret Argo CD already matches
repositories against, and otherwise stays out of the path. Argo CD's
configuration is the estate's; the updater rewrites one key of it.

## The image is where a fresh cluster can reach

The updater's image is published to GHCR, not to the registry it
refreshes credentials for. A cluster that has no ECR credential yet, on
day one or after a wipe, pulls the updater, and the updater's first run
seeds the credential. A refresher hosted behind the credential it
refreshes cannot bootstrap.

## Chart and image move together

One tag stamps the chart, the image and the binary, and the chart's
default image tag is its own `appVersion`. A released chart therefore
pulls the image built from the same commit, and a consumer pins one
version. `image.tag` exists for a mirror or a test, not for pairing a
chart with another release's image.

## Credentials come in, one key goes out

- **AWS access is the estate's.** The binary uses the SDK's default
  chain and nothing else: no key file, no static credential value. EKS
  Pod Identity, IRSA or an instance profile all work, and the chart
  carries only the annotation slot IRSA needs.
- **The token is written, never rendered.** No value holds a token; the
  chart renders no Secret data. The token exists in the run's memory and
  in the Secret's `password`, and nowhere else: not in an argument, an
  environment variable or a log line.
- **The Secret's shape is Argo CD's.** When the updater creates a
  Secret it writes exactly the fields Argo CD documents for a Helm OCI
  repo-creds entry; when it patches one it rewrites `password`. The rest
  of the Secret is the estate's, and a hand-made Secret with the right
  name is adopted, not replaced.

## Least privilege, stated in the render

The Role's patch rule names exactly the Secrets in `registries`, derived
in the template from the same list the CronJob is given, so the two
cannot disagree. `create` is unscoped only because RBAC has no way to
scope it, and the comment in the template says so. A reviewer reading
the render sees what the workload may touch without reading the code.

## Only what an estate decides

The values are the things an estate must decide: which Secrets, which
URLs, which region, how often, where the pods run, how they get AWS
credentials. Everything else (histories, TTLs, the security context, the
resource envelope, the hook's lifecycle) is fixed, because a chart with
a knob for each of them is a chart nobody can review. A fixed thing that
turns out to need a knob becomes one in a minor release, with a default
that renders what rendered before.

## Ownership contract

| This repository | The consuming estate |
| --- | --- |
| the CronJob, the seed hook, the ServiceAccount, the Role and its scoping | the namespace, the IAM role and how the pods reach it, the repository policies |
| the shape of a Secret it creates and the one key it rewrites | which Secrets exist and what else is in them |
| that a values typo fails the render | the schedule, the region, the registries, the scheduling |
| object names, stable across releases | network policy and everything Argo CD does with the credential |

## Rules a change must keep

- **A particular is an input.** An account, a registry host, a role ARN
  or a Secret name in a default is a leak; `hack/leak-canary.sh` catches
  the shapes it can.
- **A new capability renders nothing until asked for.** An existing
  values file renders byte-for-byte the same, unless the release says
  otherwise (see [adoption.md](adoption.md#the-zero-diff-gate)).
- **Names are a contract.** A changed object name, selector or
  ServiceAccount name is a major version: a Pod Identity association
  points at the ServiceAccount by name.
- **A new refusal comes with its fixture** in
  `tests/invalid/argocd-ecr-updater/`.
- **The token never appears in a render, an argument or a log.**

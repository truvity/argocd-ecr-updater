# argocd-ecr-updater

Keeps the ECR credential in Argo CD's repo-creds Secrets fresh: a CronJob
that rewrites each Secret's password with a new authorization token, and
a PostSync hook that seeds the Secrets a fresh cluster does not have yet.

| Artifact | What | Where | Status |
| --- | --- | --- | --- |
| `charts/argocd-ecr-updater` | the CronJob, the seed hook, the ServiceAccount and a Role scoped to the configured Secrets | `oci://ghcr.io/truvity/charts/argocd-ecr-updater` | shipped |
| `updater` image | the binary in a distroless image, `linux/amd64` and `linux/arm64` | `ghcr.io/truvity/argocd-ecr-updater/updater` | shipped |
| `ecr-updater` binary | the same binary as an archive per OS and architecture | the GitHub Release | shipped |
| `github.com/truvity/argocd-ecr-updater` | the Go module the binary is built from; `pkg/updater` is not a supported API | | not offered |

One tag releases all of them at the same version, and the chart's
default image tag is its own `appVersion`, so a released chart pulls the
image built from the same commit.

## Who it is for

A platform team that runs **Argo CD** on **Kubernetes** and pulls Helm
charts from **Amazon ECR**. ECR authorization tokens expire twelve hours
after they are issued and Argo CD has no refresher of its own; this is
the refresher. The pods reach AWS through the SDK's default chain, so
EKS Pod Identity, IRSA (on EKS or through the pod identity webhook
elsewhere) and an instance profile all work, and the IAM role, the
association or annotation, and the registries' repository policies are
the estate's. Nothing here installs Argo CD, creates an IAM role, or
touches a registry: it needs `ecr:GetAuthorizationToken` and a
namespace to write Secrets in.

## The model

Three nouns. A **registry** is an `oci://` URL Argo CD matches chart
sources against. A **repo-creds Secret** is the Argo CD object that
holds the credential for that URL prefix, in the Argo CD namespace. The
**token** is what ECR hands out for twelve hours, and one token
authenticates the caller against every registry it may pull from.

```
registries[]  (values)          one CronJob, every six hours
  secret ──► repo-creds Secret ◄── password rewritten, nothing else
  url    ──► written on seed   ◄── PostSync hook, on every sync:
                                    missing Secret created, existing one patched
```

The chart turns the `registries` list into three things that cannot
disagree: the Secret names the Role may patch, the names the CronJob is
given, and one seed container per registry in the hook.

## Install and a worked example

```sh
helm install argocd-ecr-updater oci://ghcr.io/truvity/charts/argocd-ecr-updater \
  --version <version> --namespace argocd --values values.yaml
```

```yaml
# values.yaml — every value is a placeholder
awsRegion: eu-example-1
registries:
  - secret: ecr-repo-creds-example
    url: oci://<account>.dkr.ecr.<region>.amazonaws.com
# With EKS Pod Identity, associate the role with the ServiceAccount
# argocd-ecr-updater in this namespace and leave the annotation out.
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: <the role's ARN>
# The defaults pin the pods to arm64 nodes; clear both on a cluster
# without that convention.
nodeSelector: {}
tolerations: []
```

As an Argo CD Application the first sync creates the Secret through the
PostSync hook, and every later sync refreshes it; between syncs the
CronJob does. An estate that already has the Secret keeps it: only its
`password` is rewritten. [docs/reference.md](docs/reference.md) has
every value and every flag.

## Documentation

- [docs/adoption.md](docs/adoption.md): prerequisites, install order,
  the zero-diff gate, adopting Secrets and refreshers that already exist,
  and upgrading
- [docs/safety.md](docs/safety.md): every refusal and what it prevents;
  the refresh cadence, what happens when a run fails, the seed hook's
  lifecycle, and the traps
- [docs/reference.md](docs/reference.md): every chart value, what the
  chart fixes, every flag and environment variable, and the Secret the
  run writes
- [docs/doctrine.md](docs/doctrine.md): what this repository owns and
  what the consuming estate owns, and why it is shaped this way
- [CHANGELOG.md](CHANGELOG.md): what changed for a consumer, per version

## The rule that makes this repository public

**Mechanism only.** Nothing here names an account, a registry host, a
role, a cluster or a Secret that is anyone's in particular. Every such
thing is an input with a neutral default, and the consuming estate
supplies it from its own (private) repository. `hack/leak-canary.sh`
enforces this in CI, and public history cannot be unpublished, so the
rule is mechanical, not remembered.

This repository follows the shared
[component contract](https://github.com/truvity/ci-workflows/blob/master/docs/component-contract.md).

## Status

Used in production by its maintainers. Releases are listed on the
[releases page](https://github.com/truvity/argocd-ecr-updater/releases),
and [CHANGELOG.md](CHANGELOG.md) says what changed for a consumer in
each.

## Development

```sh
devbox shell        # or direnv
just check          # build + lint + golden renders and go test + leak canary + govulncheck
just race           # the tests under the race detector (needs a C toolchain)
just golden         # regenerate tests/golden after a template change — review the diff
```

CI runs `build`, `lint`, `test` and `leak-canary`, each as its own job,
`race` as a job of its own, and `vuln` daily. Every
`tests/cases/argocd-ecr-updater/<case>/values.yaml` is rendered and
compared byte-for-byte with `tests/golden/argocd-ecr-updater/<case>.yaml`;
a template change is reviewed as a diff, with no cluster involved.

`tests/invalid/argocd-ecr-updater/` holds one fixture per refusal. Each
must fail to render; `just lint` proves it. A rule without a fixture is
a rule that will quietly stop working.

## Releasing

Push a tag `vX.Y.Z`. The shared release workflow creates the GitHub
Release with the binary archives, pushes the image and the chart at that
version (the chart's own `version` field is a placeholder that never
moves).

Auto-release is **armed** (`vars.AUTO_RELEASE` is `true`) and cuts
**patches only**: at once for a merged `security`-labelled pull request,
weekly when dependency bumps have moved `master` past the latest tag.
Minors and majors are manual, tagged when the change merges and after
its CHANGELOG heading. The weekly lane asks only whether `master` moved,
not what moved it, so a feature merged and left untagged ships in the
next weekly patch: tag the minor when the feature merges.

## Licence

MIT — see [LICENSE](LICENSE).

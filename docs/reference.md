# Reference

Every chart value, every flag and every environment variable the binary
reads. `charts/argocd-ecr-updater/values.yaml` carries the same keys as
commented defaults and `values.schema.json` is the authority on types: an
unknown key fails the render. [safety.md](safety.md) lists every refusal.

## charts/argocd-ecr-updater

Renders, in the release namespace: a ServiceAccount, a Role and
RoleBinding, a CronJob, and, when `registries` is non-empty, a Job
annotated as an Argo CD PostSync hook. Nothing else: no Secret (the
updater writes those at run time), no NetworkPolicy, no Service.

### Values

| Value | Default | Type | Notes |
| --- | --- | --- | --- |
| `image.repository` | `ghcr.io/truvity/argocd-ecr-updater/updater` | string, non-empty | the image the release publishes; on GHCR so a fresh cluster can pull it before any ECR credential exists |
| `image.tag` | `""` | string | empty: the chart's `appVersion`, which the release stamps, so a released chart pulls its own image |
| `image.pullPolicy` | `IfNotPresent` | `Always`, `IfNotPresent` or `Never` | |
| `schedule` | `0 */6 * * *` | cron expression, non-empty | when the CronJob refreshes; ECR tokens live twelve hours, see [safety.md](safety.md#the-refresh-cadence) |
| `awsRegion` | `eu-central-1` | string, non-empty | the region whose ECR endpoint the token is requested from, passed as `--region` |
| `registries` | `[]` | list | one entry per repo-creds Secret; **required** in practice, see the row below and [safety.md](safety.md#an-empty-registries-list) |
| `registries[].secret` | | string, non-empty | the name of the Argo CD repo-creds Secret; the CronJob patches it, the hook creates it, and the Role's patch rule names it |
| `registries[].url` | | string, `oci://…` | the registry URL written into the Secret when the hook creates it; the Argo CD `url` field of a Helm OCI repo-creds entry |
| `serviceAccount.annotations` | `{}` | map of strings | for IRSA, `eks.amazonaws.com/role-arn`; with EKS Pod Identity nothing is needed here |
| `nodeSelector` | `kubernetes.io/arch: arm64` | map of strings | on the CronJob pod and the hook pod; `{}` removes the block |
| `tolerations` | one `arch=arm64:NoSchedule` toleration | list | passed through; `[]` removes the block |
| `global` | unset | object | accepted so a parent chart's globals do not fail the schema; unused |

### What the chart fixes, not values

- **CronJob:** `concurrencyPolicy: Forbid`, one successful and three
  failed Jobs kept, finished Jobs deleted after an hour
  (`ttlSecondsAfterFinished: 3600`), pod `restartPolicy: OnFailure`, a
  30-second grace period. No `activeDeadlineSeconds` and no
  `startingDeadlineSeconds`.
- **The CronJob container** (`ecr-token-refresh`) runs
  `--region=<awsRegion> --namespace=<release namespace>
  --secrets=<every registries[].secret, comma-joined>`: one token, every
  Secret patched.
- **The hook Job** (`argocd-ecr-updater-seed`): annotations
  `argocd.argoproj.io/hook: PostSync` and
  `argocd.argoproj.io/hook-delete-policy: BeforeHookCreation`, deleted
  five minutes after it finishes. One container per registry, named
  `seed-<secret>` with a leading `ecr-repo-creds-` stripped from the
  Secret name, each running `--region --namespace --secrets=<that secret>
  --registry-url=<its url> --seed`.
- **Resources,** both pods: requests `cpu: 10m`, `memory: 64Mi`; limit
  `memory: 128Mi`.
- **Security context,** both pods: non-root, user 65534, read-only root
  filesystem, `RuntimeDefault` seccomp, no privilege escalation, every
  capability dropped.
- **RBAC:** one Role with two rules on `secrets`: `get`, `list`, `patch`,
  `update` with `resourceNames` set to exactly the `registries[].secret`
  list, and `create` unscoped, because RBAC cannot scope a create by name.

### Names and labels

Every object is named `argocd-ecr-updater` (the hook Job
`argocd-ecr-updater-seed`) and carries the single label
`app.kubernetes.io/name: argocd-ecr-updater`. Names do not carry the
release, so the chart installs **once per namespace**; a second release
in the same namespace collides on every object. The ServiceAccount is
always named `argocd-ecr-updater`: an EKS Pod Identity association
targets that name in the release namespace.

## The binary: `ecr-updater`

One run: obtain one ECR authorization token, then for each Secret named
in `--secrets`, patch its `password` if it exists, create it if it does
not and `--seed` is set, and otherwise log a warning and move on. Exit 0
when every Secret was handled, 1 on the first error. Logs are JSON on
stderr.

### Flags

| Flag | Default | Notes |
| --- | --- | --- |
| `--region` | required | the AWS region whose ECR endpoint issues the token |
| `--namespace` | `argocd` | the namespace of the Secrets |
| `--secrets` | `ecr-repo-creds-preview,ecr-repo-creds-stable` | comma-separated Secret names; blanks are skipped. The chart always sets this, so the default only matters when the binary is run by hand |
| `--registry-url` | `""` | written as the Secret's `url` when the run creates it; ignored when patching |
| `--seed` | off | create a missing Secret instead of warning about it |
| `--version` | | the release version and commit, stamped at build time |

### Environment

The binary reads no configuration from the environment of its own. Two
SDKs do:

- **AWS** (`aws-sdk-go-v2`, the default credential chain, region forced
  by `--region`): static keys (`AWS_ACCESS_KEY_ID`,
  `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`), a web identity
  (`AWS_ROLE_ARN` and `AWS_WEB_IDENTITY_TOKEN_FILE`, which the IRSA
  webhook injects), a container credential endpoint
  (`AWS_CONTAINER_CREDENTIALS_FULL_URI` and
  `AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE`, which EKS Pod Identity
  injects), then the instance profile. The principal needs
  `ecr:GetAuthorizationToken`, which is only ever granted on `*`.
- **Kubernetes** (`client-go`, in-cluster configuration only):
  `KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT` and the projected
  ServiceAccount token. There is no kubeconfig mode; the binary is meant
  to run in the cluster it writes to.

### The Secret the run writes

When it creates a Secret, the run writes what Argo CD expects of a Helm
OCI repo-creds entry:

```yaml
metadata:
  labels:
    argocd.argoproj.io/secret-type: repo-creds
    argocd-ecr-updater: enabled
stringData:
  type: helm
  url: <--registry-url>
  enableOCI: "true"
  username: AWS
  password: <the token>
```

When it patches one, it rewrites `password` only, with a strategic merge
patch, so any other key the estate put on the Secret stays.

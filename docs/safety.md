# Safety — what can break, and what this repository does about it

The failure this component exists for is silent: an ECR authorization
token expires twelve hours after it was issued, nothing in Argo CD
refreshes it, and the first sign is a repo-server that can no longer
fetch a chart, some hours after the last person touched anything. So the
component refreshes on a schedule, seeds on every sync, and refuses at
render time the values mistakes it can see.

## The chart: refused at render time

`charts/argocd-ecr-updater` has no render-time `fail`. Every refusal is
its `values.schema.json`, which Helm checks on every `lint`, `template`,
`install` and `upgrade`, and so does a GitOps controller that renders the
chart. Each rule has a fixture under `tests/invalid/argocd-ecr-updater/`,
and `just lint` renders every fixture and fails if one renders. It also
renders the chart with `--set bogusKey=1` and fails if that renders.

| Fixture | Values | What it prevents |
| --- | --- | --- |
| `unknown-key.yaml` | `schedul` next to `schedule` | a typo read as "use the default": the CronJob runs every six hours and the values file says otherwise |
| `image-unknown-key.yaml` | `image.version` | the pinned image running while the values file names another version |
| `image-missing.yaml` | `image: null` | a render with no image block at all |
| `image-repository-empty.yaml` | `image.repository: ""` | a pod spec the API server rejects after the rest of the release applied |
| `pull-policy-invalid.yaml` | `image.pullPolicy: Sometimes` | the same |
| `schedule-empty.yaml`, `schedule-missing.yaml` | `schedule: ""`, `schedule: null` | a CronJob the API server rejects, so no refresh ever runs |
| `aws-region-empty.yaml`, `aws-region-missing.yaml` | `awsRegion: ""`, `null` | `--region=` on every run; the binary requires it and would fail every run |
| `registries-missing.yaml` | `registries: null` | the key dropped entirely; see [an empty list](#an-empty-registries-list) for why `[]` is refused nowhere yet |
| `registries-not-a-list.yaml` | a map of name to URL | the list the Role, the CronJob and the hook are all derived from, in a shape none of them can read |
| `registry-unknown-key.yaml` | `registries[].region` | a per-registry setting the chart has no field for, silently ignored: the token comes from `awsRegion` for every registry |
| `registry-secret-missing.yaml`, `registry-secret-empty.yaml` | no `secret`, `secret: ""` | a Role rule naming an empty Secret, and a run told to patch nothing |
| `registry-url-missing.yaml` | no `url` | a Secret seeded without the `url` Argo CD matches repositories against |
| `registry-url-not-oci.yaml` | `url: https://…` | a Secret Argo CD would not use as a Helm OCI repo-creds entry; the seed writes `enableOCI: "true"` and the URL must agree |
| `serviceaccount-unknown-key.yaml` | `serviceAccount.name` | a setting the chart has no field for: the ServiceAccount is always named `argocd-ecr-updater`, and a Pod Identity association targets that name |
| `serviceaccount-annotation-not-a-string.yaml` | a numeric annotation value | an object the API server rejects |
| `nodeselector-not-a-string.yaml` | a list as a selector value | the same |
| `tolerations-not-a-list.yaml` | a map as `tolerations` | the same |

Strictness stops where Kubernetes' own fields begin: `tolerations` is
passed through unchecked, and the API server validates it.

## The refresh cadence

An ECR authorization token is valid for **twelve hours** from the moment
it is issued. The CronJob's default schedule is `0 */6 * * *`, every six
hours, so every token is replaced with half its life left, and **one
failed run is survivable**: the previous token still has six hours, and
the next run replaces it. Two consecutive failures expire the
credential.

A schedule of more than twelve hours guarantees a window with an expired
token on every cycle; the schema does not refuse it, because the cron
expression is not parsed at render time. Keep the interval under six
hours if the estate wants the one-failure margin.

The token is requested against the `awsRegion` endpoint. AWS documents
the token as valid for any registry the principal may access, so one
run authenticates every registry in the list; nothing here is
per-registry except the Secret it lands in.

## What happens when the CronJob fails

- **In the pod:** `restartPolicy: OnFailure`, so a container that exits
  non-zero (no credentials, a denied `GetAuthorizationToken`, a Secret
  patch the Role does not allow) is restarted in place, with the Job
  controller's default back-off. After the default six retries the Job
  is marked failed and kept, with two more like it
  (`failedJobsHistoryLimit: 3`), for `kubectl logs`.
- **For the credential:** nothing changes yet. The Secret still holds the
  previous token, good until twelve hours after it was issued. The
  refresh has six hours of margin at the default schedule.
- **After expiry:** Argo CD's repo-server gets an authentication error
  from ECR on its next fetch. Applications that reference the registry
  report a comparison error; what already runs in the cluster is
  untouched, because the cluster's own image pulls do not use this
  Secret. A hard refresh of the Application, or a new revision, will not
  help until the token is replaced.
- **Recovery:** the next successful run patches the password and Argo CD
  picks it up on its next fetch. To not wait for the schedule:
  `kubectl -n <ns> create job --from=cronjob/argocd-ecr-updater
  refresh-now`. The run only ever rewrites `password`, so there is
  nothing to clean up.
- **A hung run blocks the next one.** `concurrencyPolicy: Forbid` skips a
  scheduled run while the previous Job is still active, and the CronJob
  sets no `activeDeadlineSeconds`. A pod stuck waiting on a credential
  endpoint that never answers therefore blocks every later refresh until
  it is deleted. Alert on a Job older than the schedule interval.

A Secret that is not there is **not** an error for the CronJob: the run
logs `secret not found, token refresh skipped` and continues with the
next name. A repo-creds Secret deleted by hand stays gone until the next
sync runs the seed hook (below).

## The PostSync seed hook

Rendered only when `registries` is non-empty, and marked for Argo CD as a
`PostSync` hook with `hook-delete-policy: BeforeHookCreation`:

- **On every sync** of the Application that carries this chart, Argo CD
  deletes the previous hook Job (if it is still there; finished ones are
  deleted after five minutes anyway) and creates a new one. One container
  per registry runs with `--seed` and that registry's `--registry-url`:
  a Secret that is missing is created with Argo CD's repo-creds shape
  and a fresh token; one that exists has its password patched, the same
  as the CronJob does. So a sync is also a refresh.
- **A failing hook fails the sync.** Argo CD marks the sync operation
  Failed when a PostSync hook Job fails, and the Application shows it.
  The resources synced before the hook (the CronJob, the Role, the
  ServiceAccount) stay applied. The hook fails for the same reasons the
  CronJob does, and one more: it is the first thing to run after a fresh
  install, so it is where a missing Pod Identity association or IRSA
  annotation shows up. Fix the credential path and sync again.
- **Under plain Helm** the annotations mean nothing: `helm install`
  creates the Job as an ordinary Job at install time, it runs once, and
  it is deleted five minutes after it finishes. A `helm upgrade` that
  changes the Job's template while a previous Job of that name is still
  present fails on the Job's immutable spec; wait for the TTL or delete
  it. The chart is written for Argo CD, where the hook lifecycle is
  Argo CD's.
- **The create race** between the hook's `get` and its `create` (the
  CronJob running at the same moment, or two syncs) is handled: an
  `AlreadyExists` falls through to the patch.

## Traps worth knowing

### An empty `registries` list

`registries: []` renders. The hook is skipped, the CronJob runs with
`--secrets=` and does nothing, and the Role's first rule renders
`resourceNames:` with **no names**, which RBAC reads as "every Secret in
the namespace". The ServiceAccount then holds `get`, `list`, `patch` and
`update` on all of them, for a workload that touches none. Set
`registries` before installing. A `minItems` refusal is the right fix
and is a separate release, because it changes what renders today.

### The `create` verb is unscoped

RBAC cannot restrict a `create` by name, so the second Role rule grants
`create` on every Secret in the namespace. It is what the seed needs,
and the Role is namespaced, so the grant reaches the release namespace
and nothing beyond it.

### The hook's container name is derived from the Secret name

Each hook container is named `seed-` plus the Secret name with a leading
`ecr-repo-creds-` removed. A container name must be a DNS label (lower
case, digits, hyphens, at most 63 characters), so a Secret named with a
dot, or whose name exceeds the limit after the prefix, renders a Job the
API server rejects, and the sync fails on the hook. Name the Secrets
`ecr-repo-creds-<label>`.

### One install per namespace

Every object is named `argocd-ecr-updater`, without the release name.
Two releases in one namespace fight over the same CronJob and Role. Two
Argo CD instances in two namespaces are two installs, each with its own
`registries`.

### The scheduling defaults assume an arch taint

`nodeSelector` pins the pods to `arm64` nodes and `tolerations` tolerates
an `arch=arm64:NoSchedule` taint. On a cluster without that convention
the pods are unschedulable and every refresh is `Pending`. Set both to
empty (`nodeSelector: {}`, `tolerations: []`), as the `unpinned` golden
case does.

### The Secret is the estate's, apart from `password`

The updater creates a Secret only when it is missing, and afterwards
rewrites one key. An estate that manages the Secret itself (External
Secrets, a Pulumi program) must expect the password to change under it
every six hours; a tool that reconciles the whole Secret back to a stored
value will fight the updater and lose the token. Own the Secret, or own
its `password`, not both.

### Network policy is the estate's

The chart renders no NetworkPolicy. The pods need egress to the ECR and
STS endpoints of `awsRegion` (or the credential endpoint the Pod
Identity agent serves on the node) and to the Kubernetes API. A
default-deny namespace without that egress fails every run.

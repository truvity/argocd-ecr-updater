# Adoption

## Prerequisites

- **Argo CD** in the cluster, and the namespace it reads repo-creds
  Secrets from (`argocd` unless the estate moved it). The chart installs
  into that namespace: the Role is namespaced and the Secrets must be
  where Argo CD looks.
- **An AWS principal for the pods** with `ecr:GetAuthorizationToken`
  (granted on `*`; ECR accepts nothing narrower), reachable through the
  SDK's default chain: an EKS Pod Identity association for the
  ServiceAccount `argocd-ecr-updater` in the release namespace, or an
  IRSA role whose ARN goes in `serviceAccount.annotations`. On a
  non-EKS cluster, IRSA works with the pod identity webhook the estate
  runs. Nothing here creates the role or the association.
- **Registries** whose repository policies grant that principal pull, and
  the `oci://` URL of each as Argo CD will reference it.
- **Helm 3** with OCI support to pull the chart, and `batch/v1` CronJobs.
- **Egress** from the pods to the ECR and STS endpoints of `awsRegion`,
  or to the node-local credential endpoint, and to the Kubernetes API.

## Install order

1. **The principal.** The IAM role and, for EKS Pod Identity, the
   association to `argocd-ecr-updater` in the release namespace. The
   association may exist before the ServiceAccount does.
2. **The chart,** with `registries` set: one entry per repo-creds
   Secret. As an Argo CD Application, the first sync applies the
   ServiceAccount, Role, RoleBinding and CronJob, then runs the PostSync
   hook, which creates each Secret with a fresh token. With plain Helm
   the hook Job runs once at install.
3. **The repositories.** Argo CD Applications whose chart source is one
   of the `oci://` URLs now match the repo-creds Secret by URL prefix and
   authenticate with the token. Nothing else references the updater.

A fresh cluster can do all of this before it holds any ECR credential:
the image is on GHCR, so the seed hook pulls without one.

## The zero-diff gate

**A consumer adopts a release only when the render it produces is
byte-identical to what runs, or differs exactly by the change the
release announces** in [CHANGELOG.md](../CHANGELOG.md).

Render your values at the pinned version and at the new one, and
compare:

```sh
helm template argocd-ecr-updater oci://ghcr.io/truvity/charts/argocd-ecr-updater \
  --version <pinned> --namespace argocd -f values.yaml > old.yaml
helm template argocd-ecr-updater oci://ghcr.io/truvity/charts/argocd-ecr-updater \
  --version <new> --namespace argocd -f values.yaml > new.yaml
diff old.yaml new.yaml
```

Between two releases that announce nothing for the chart, the only
line that moves is the image tag, which follows the chart's stamped
`appVersion`. Anything else is a reason to stop and read the CHANGELOG
again.

Moving from a hand-written CronJob to this chart is one change whose
render, against what runs, differs only in what the CHANGELOG for that
version says. Tightening something afterwards (a shorter schedule, a
narrower `nodeSelector`) is a separate change, adopted on its own
evidence.

## Adopting what already exists

### Repo-creds Secrets the estate already has

The updater adopts them. Name each in `registries[].secret`; on the next
sync the hook finds the Secret, and on every run afterwards the CronJob
rewrites its `password` and nothing else. The Secret's `type`, `url`,
`username`, `enableOCI` and labels stay as the estate wrote them; the
`argocd-ecr-updater: enabled` label is set only on Secrets the updater
created. Check that the existing Secret's `username` is `AWS` and its
`url` is the `oci://` URL Argo CD matches on; the updater does not
correct either.

A tool that reconciles the whole Secret (an external-secrets controller,
a Pulumi program) has to stop owning `password`, or the two fight; see
[safety.md](safety.md#the-secret-is-the-estates-apart-from-password).

### A refresher the estate wrote by hand

Delete the hand-written CronJob and its RBAC in the same change that
installs the chart, and install the chart with the same `registries`.
The Secrets are unaffected: the chart's objects have their own names,
and the Secrets are found by name, not created anew. The only window is
between the old CronJob's last run and the hook's first, which is a
single sync.

## Upgrading

### v1.0.0 to v1.0.1: the values schema

The chart gained `values.schema.json`. An unknown key in your values now
fails the render. Render your values once before adopting and remove or
correct any key the chart does not have; `registries[].url` must start
with `oci://`.

No release so far has changed an object name, a selector or a default;
there is no upgrade that needs a step beyond the gate above.

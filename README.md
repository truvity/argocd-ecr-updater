# argocd-ecr-updater

ECR token refresher for ArgoCD repo-creds Secrets: an hourly-ish
CronJob plus a PostSync seed hook. ECR authorization tokens expire
after 12 hours and ArgoCD has no native refresher — this closes the
gap, and because the image lives on ghcr, a fresh cluster can pull it
before any ECR credential exists.

## How it works

- The **CronJob** (default `0 */6 * * *`) obtains one ECR authorization
  token and patches the `password` field of each configured repo-creds
  Secret. One token authenticates against any registry whose repo
  policies grant the caller.
- The **PostSync hook** runs on every sync and creates each configured
  Secret that does not exist yet (`--seed`), so first deploy on a fresh
  cluster needs no manual seeding.
- RBAC is scoped: patch/update applies to exactly the configured Secret
  names; only `create` is unscoped (RBAC cannot scope creates by name).

## Install

```sh
helm install argocd-ecr-updater \
  oci://ghcr.io/truvity/charts/argocd-ecr-updater \
  --namespace argocd \
  --set awsRegion=eu-central-1 \
  --set 'registries[0].secret=ecr-repo-creds-stable' \
  --set 'registries[0].url=oci://<account>.dkr.ecr.<region>.amazonaws.com'
```

## AWS access

Auth-agnostic — the workload uses the SDK default chain and needs only
`ecr:GetAuthorizationToken`:

- **EKS Pod Identity**: create a pod identity association for
  ServiceAccount `argocd-ecr-updater` in the release namespace.
- **IRSA** (plain EKS, or non-EKS clusters via
  [amazon-eks-pod-identity-webhook](https://github.com/truvity/amazon-eks-pod-identity-webhook)):
  `--set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::<acct>:role/<name>`

## Releases

One `v*` tag releases the image
(`ghcr.io/truvity/argocd-ecr-updater/updater`) and the chart
(`ghcr.io/truvity/charts/argocd-ecr-updater`) at the same version; the
chart's default image tag is its own appVersion, so every released
pair is self-consistent.

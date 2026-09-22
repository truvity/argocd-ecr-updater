# Changelog

What changed for a consumer, per version, newest first. A version with no
heading here is a patch cut automatically for dependency bumps alone; its
GitHub Release lists them. The chart and the image are released together
at every version.

## v1.0.1

- **`values.schema.json`: an unknown key fails the render.** Strict at
  the top level and inside `image`, each `registries` entry and
  `serviceAccount`; `registries[].url` must start with `oci://`.
  `serviceAccount.annotations` and `nodeSelector` accept any string
  keys, and `tolerations` is passed through. A values file the chart
  accepted before renders the same, unless it carried a key the chart
  never read.

## v1.0.0

- First release: the `argocd-ecr-updater` chart and the `updater` image
  at one version. A CronJob refreshes the ECR token in each configured
  repo-creds Secret; a PostSync hook seeds the Secrets a fresh cluster
  does not have yet. The Role's patch rule names exactly the configured
  Secrets; `serviceAccount.annotations`, `nodeSelector` and
  `tolerations` are values; the hook renders only when `registries` is
  set.

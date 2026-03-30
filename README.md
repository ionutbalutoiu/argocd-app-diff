# argocd-app-diff

`argocd-app-diff` compares a local Argo CD `Application` manifest with the live resources
managed by Argo CD.

It exists for a narrower workflow than the upstream `argocd` CLI: reviewing changes to the
`Application` YAML itself.

## Why

The upstream `argocd app diff` flow is awkward for this use case:

- it is centered on an existing app name, not a local `Application` file
- `--local` expects local manifests, not an `Application` that still needs Argo CD to
  resolve and render
- multi-source applications are a poor fit for the upstream local diff path

This tool keeps the input simple:

- give it a single local `Application` manifest with `--application-file`
- let Argo CD render the target state
- diff that rendered state against what is live

The goal is a diff that is closer to what Argo CD actually sees, without forcing the
review workflow through `argocd app diff APPNAME --local ...`.

## Requirements

You need:

- access to the Argo CD API
- access to the Argo CD repo-server via `--repo-server-url` or `ARGOCD_REPO_SERVER_URL`
- a local file containing exactly one `argoproj.io/v1alpha1` `Application`

`--repo-server-url` must use `grpc://` or `grpcs://`.

## Usage

Build:

```sh
make build
```

Run:

```sh
./bin/argocd-app-diff \
  --repo-server-url grpcs://argocd-repo-server.argocd.svc:8081 \
  --application-file path/to/application.yaml
```

Override one or more source revisions:

```sh
./bin/argocd-app-diff \
  --repo-server-url grpcs://argocd-repo-server.argocd.svc:8081 \
  --application-file path/to/application.yaml \
  --source-revision-override https://github.com/example/platform-config.git|feature-branch \
  --hard-refresh
```

Useful flags:

- `--namespace`
- `--refresh`
- `--hard-refresh`
- `--diff-exit-code`

Exit codes:

- `0` for no diff
- `1` for diff found by default
- `2` for general CLI errors

The diff output is compatible with Argo CD and honors `KUBECTL_EXTERNAL_DIFF`.

## Scope

- diff only; no sync or cluster mutation
- intentionally focused on local `Application` review, not full upstream CLI parity

## Development

- `make fmt`
- `make build`
- `make test`
- `make lint`

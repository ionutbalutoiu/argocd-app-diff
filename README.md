# argocd-app-diff

`argocd-app-diff` compares a local Argo CD `Application` manifest with the live resources
managed by Argo CD.

It is built for a narrower workflow than the upstream `argocd` CLI: reviewing changes to the
`Application` YAML itself before they reach the cluster.

## Why This Exists

The upstream `argocd app diff` command works well when you already have an application name
and local rendered manifests. It is less convenient when your starting point is a local
`Application` manifest that Argo CD still needs to resolve and render.

In that workflow:

- the command is centered on an existing app name, not a local `Application` file
- `--local` expects rendered manifests, not an `Application` that Argo CD still needs to
  resolve
- multi-source applications do not fit especially well into the upstream local diff flow

This tool keeps the input simple:

- point it at a single local `Application` manifest with `--application-file`
- let Argo CD render the target state
- compare that rendered state with what is currently live

The goal is a diff that is closer to what Argo CD actually evaluates, without forcing the
review flow through `argocd app diff APPNAME --local ...`.

## Requirements

You need:

- access to the Argo CD API server
- access to the Argo CD repo-server, provided with `--repo-server-url` or
  `ARGOCD_REPO_SERVER_URL`
- a local file containing exactly one `argoproj.io/v1alpha1` `Application` object

`--repo-server-url` must use either `grpc://` or `grpcs://`.

## Usage

Build the binary:

```sh
make build
```

Run a diff:

```sh
./bin/argocd-app-diff \
  --repo-server-url grpcs://argocd-repo-server.argocd.svc:8081 \
  --application-file path/to/application.yaml
```

Override one or more source revisions before diffing:

```sh
./bin/argocd-app-diff \
  --repo-server-url grpcs://argocd-repo-server.argocd.svc:8081 \
  --application-file path/to/application.yaml \
  --source-revision-override https://github.com/example/platform-config.git|feature-branch \
  --hard-refresh
```

You can repeat `--source-revision-override` to override multiple repositories.

Common flags:

- `--namespace` overrides the namespace used to look up the live `Application`
- `--refresh` refreshes application data before reading live resources
- `--hard-refresh` also bypasses the repo-server manifest cache
- `--exit-code=false` always returns `0`, even when a diff is found
- `--diff-exit-code` customizes the exit code returned when a diff is found

Exit codes:

- `0` when no diff is found
- `1` when a diff is found, by default
- `2` for general CLI errors

The diff output is compatible with Argo CD and honors `KUBECTL_EXTERNAL_DIFF`.

## Scope

This tool is intentionally narrow:

- diff only; it does not sync or mutate cluster state
- focused on reviewing a local `Application`, not matching the full upstream `argocd` CLI
  surface

## Development

- `make fmt`
- `make build`
- `make test`
- `make lint`

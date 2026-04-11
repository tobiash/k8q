# k8q

A Unix-style pipe for filtering, mutating, and exploring streams of Kubernetes YAML manifests.

`k8q` reads YAML from stdin, applies transformations, and writes the result to stdout. It understands Kubernetes semantics — kind matching, API groups, workload pod templates — so you can manipulate manifests safely without breaking formatting or losing comments.

Output is automatically formatted: fields are reordered to follow Kubernetes conventions (apiVersion, kind, metadata, spec, status), and colorized when writing to a terminal.

Built on `kustomize/kyaml` for lossless AST-based YAML manipulation.

## Install

```bash
go install github.com/tobiash/k8q@latest
```

## Commands

### `get` -- Filter manifests

Filters the stream to keep only manifests matching the given criteria. All provided criteria must match (AND semantics).

```bash
# Get all ConfigMaps
helm template my-chart | k8q get --kind ConfigMap

# Get a specific deployment by name
helm template my-chart | k8q get --kind Deployment --name my-app

# Filter by API group
helm template my-chart | k8q get --group apps

# Filter by label selector
helm template my-chart | k8q get -l app=web
```

### `drop` — Remove manifests

Filters the stream to remove manifests matching the given criteria. Manifests matching ANY provided criterion are dropped (OR semantics).

```bash
# Remove all Flux CD resources by API group
kustomize build . | k8q drop --group toolkit.fluxcd.io

# Remove resources by multiple criteria (OR)
helm template my-chart | k8q drop --group cert-manager.io --kind ConfigMap

# Use label selectors
kustomize build . | k8q drop -l 'tier in (frontend,backend)'
```

### `subst` — Substitute environment variables

Replaces `${VAR}` references in the raw YAML stream using values from a `.env` file, before parsing.

```bash
# manifest.yaml contains ${DB_HOST} and ${DB_PORT}
cat manifest.yaml | k8q subst --env-file .env

# Works with multi-document streams
helm template my-chart | k8q subst --env-file production.env
```

### `label` — Inject labels into manifests

Adds a label to `metadata.labels` on matching manifests. For workload kinds (Deployment, DaemonSet, StatefulSet, Job), the label is also injected into `spec.template.metadata.labels`.

```bash
# Label everything
helm template my-chart | k8q label app.kubernetes.io/managed-by=k8q

# Label only Deployments
helm template my-chart | k8q label app=web --kind Deployment

# Add a version label before applying
kustomize build . | k8q label app.kubernetes.io/version=1.2.3 | kubectl apply -f -
```

### `set-namespace` — Overwrite namespace

Sets `metadata.namespace` on matching manifests in the stream.

```bash
# Deploy everything to a specific namespace
helm template my-chart | k8q set-namespace production

# Only move resources from 'default' to 'production'
helm template my-chart | k8q set-namespace production --old-namespace default

# Override namespace for a specific deployment
kustomize build . | k8q set-namespace staging --kind Deployment --name my-app
```

## Match Criteria

Both filtering (`get`, `drop`) and mutation (`label`, `set-namespace`) commands support the same matching filters:

| Flag | Shorthand | Description |
|---|---|---|
| `--kind` | | Kubernetes Kind (case-insensitive) |
| `--name` | | Resource name (exact) |
| `--namespace` | `-n` | Resource namespace (exact) |
| `--group` | `-g` | API group (substring match) |
| `--selector` | `-l` | Kubernetes label selector |
| (positional) | | kind, kind/name, or api-group |

For `get`, `label`, and `set-namespace`, multiple criteria are combined with **AND**.
For `drop`, multiple criteria are combined with **OR**.

Matching is optional for mutators (they match everything by default) but required for filters.

## Label Selectors

`get` and `drop` support Kubernetes-style label selectors via the `-l` / `--selector` flag. The syntax matches `kubectl`:

## Composition

Commands compose naturally through pipes:

```bash
# Get a deployment, labeled, in the right namespace
helm template my-chart \
  | k8q get --kind Deployment --name my-app \
  | k8q label app.kubernetes.io/managed-by=k8q \
  | k8q set-namespace production

# Strip Flux resources, substitute env vars, and apply
kustomize build . \
  | k8q drop --group toolkit.fluxcd.io \
  | k8q subst --env-file .secrets \
  | kubectl apply -f -
```

## Why not `yq`?

`yq` is a general-purpose YAML processor. `k8q` is purpose-built for Kubernetes manifests:

- **Kind-aware filtering** -- `k8q get --kind Deployment` knows what a Deployment is.
- **Label selectors** -- `-l app=web,env!=staging` with full Kubernetes selector syntax.
- **Workload-aware labeling** -- automatically propagates labels to pod templates.
- **API group matching** -- `k8q drop --group toolkit.fluxcd.io` filters by group, not string matching.
- **Canonical field ordering** -- output is automatically sorted: apiVersion, kind, metadata, spec, status.
- **Colorized output** -- syntax-highlighted YAML in the terminal, disabled when piped.
- **Formatting preservation** -- uses kyaml's AST, so comments survive.

## Tech Stack

| Component | Library |
|---|---|
| CLI framework | [alecthomas/kong](https://github.com/alecthomas/kong) |
| YAML engine | [kustomize/kyaml](https://github.com/kubernetes-sigs/kustomize/tree/master/kyaml) |
| Env substitution | [drone/envsubst](https://github.com/drone/envsubst) |

## License

See [LICENSE](LICENSE).

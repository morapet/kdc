# kdc

**Generate Docker Compose from Kustomize — use your K8s manifests for local development**

`kdc` (**K**ustomize → **D**ocker **C**ompose) is a CLI tool that generates a Docker Compose project from Kustomize manifests. It bridges the gap between Kubernetes-native configuration (Kustomize overlays) and local development (Docker Compose), keeping Kustomize as the single source of truth while giving developers the fast `docker compose up` workflow.

---

## Why kdc?

Teams that deploy to Kubernetes with Kustomize typically maintain a separate `docker-compose.yml` for local development. These two files describe the same services but diverge over time — new environment variables, changed images, and new sidecars are added to one and forgotten in the other.

**kdc eliminates this drift** by generating Docker Compose directly from your Kustomize overlays:

- One source of truth: your Kustomize overlays
- No separate `docker-compose.yml` to maintain
- No local Kubernetes cluster required (no minikube, kind, or k3d)
- Fast `docker compose up` workflow for everyone on the team

---

## How it works

```
kustomize build overlays/local  →  K8s YAML  →  kdc  →  docker-compose.yaml + .env files
```

kdc runs `kustomize build`, parses the multi-document YAML output, and translates Kubernetes resources into Docker Compose equivalents. ConfigMaps and Secrets referenced as `envFrom` are written to `.env` files under `.kdc/envs/`.

---

## Installation

### Go install

```bash
go install github.com/morapet/kdc/cmd/kdc@latest
```

### From release binaries

Download pre-built binaries from [GitHub Releases](https://github.com/morapet/kdc/releases). Binaries are available for:

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64, arm64)

### From source

```bash
git clone https://github.com/morapet/kdc.git
cd kdc
go build -o kdc ./cmd/kdc
```

### Prerequisites

`kustomize` must be installed and available in your `PATH`. Download it from [kubectl.docs.kubernetes.io/installation/kustomize](https://kubectl.docs.kubernetes.io/installation/kustomize/).

---

## Quick Start

```bash
# Generate docker-compose.yaml from your Kustomize overlay
kdc generate -k overlays/dev

# Start services
docker compose up
```

---

## CLI Reference

### `kdc generate`

Generate `docker-compose.yaml` from a Kustomize overlay.

```
kdc generate -k <path> [flags]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--kustomize` | `-k` | *(required)* | Path passed to `kustomize build` |
| `--output` | `-o` | `docker-compose.yaml` | Output path (use `-` for stdout) |
| `--overrides` | | | Optional `kdc-overrides.yaml` for compose-level overrides |
| `--filters` | | | Optional `kdc-filters.yaml` to skip or replace containers/resources |
| `--project` | | overlay dir basename | Compose project name |
| `--namespace` | | `default` | Kubernetes namespace for resource lookups |
| `--verbose` | `-v` | `false` | Print filter messages and per-resource warnings to stderr |
| `--dry-run` | | `false` | Print YAML to stdout, do not write files |

---

## What Gets Translated

### Supported translations

| Kubernetes Resource | Compose Equivalent |
|---|---|
| `Deployment` | `services.<name>` (one per container) |
| `Pod` | `services.<name>` |
| `ConfigMap` (env) | `.env` files in `.kdc/envs/` |
| `ConfigMap` (volume) | Bind mount from `.kdc/configs/` |
| `Secret` (env) | `.env` files in `.kdc/envs/` |
| `Secret` (volume) | Bind mount from `.kdc/secrets/` |
| `PersistentVolumeClaim` | Named volumes |
| Container `env` / `envFrom` | `environment:` / `env_file:` |
| Container `command` / `args` | `entrypoint:` / `command:` |
| Container `ports` | `ports:` |
| Readiness/Liveness probes | `healthcheck:` (using bash `/dev/tcp`) |
| Resource requests/limits | `deploy.resources` |
| `EmptyDir` volumes | `tmpfs` mounts |

### ConfigMap / Secret volume mounts and `subPath`

When a `volumeMount` has a `subPath`, Kubernetes mounts only that single key as a
file at `mountPath`.  kdc respects this: when `subPath` is set the bind-mount
source is narrowed to the specific file inside the `.kdc` directory rather than
the whole directory:

| Mount type | `subPath` | Compose bind source |
|---|---|---|
| `configMap` | *(absent)* | `.kdc/configs/<name>/` (directory) |
| `configMap` | `database-init.sh` | `.kdc/configs/<name>/database-init.sh` (file) |
| `secret` | *(absent)* | `.kdc/secrets/<name>/` (directory) |
| `secret` | `tls.crt` | `.kdc/secrets/<name>/tls.crt` (file) |

**Example — Postgres init script via ConfigMap `subPath`:**

```yaml
# Kubernetes manifest (abbreviated)
volumeMounts:
  - name: init-scripts
    mountPath: /docker-entrypoint-initdb.d/database-init.sh
    subPath: database-init.sh
    readOnly: true
volumes:
  - name: init-scripts
    configMap:
      name: sample-database-init-2fkb264hk4
```

kdc generates:

```yaml
volumes:
  - type: bind
    source: ./.kdc/configs/sample-database-init-2fkb264hk4/database-init.sh
    target: /docker-entrypoint-initdb.d/database-init.sh
    read_only: true
```

**Limitations:**

- `subPathExpr` (Downward API variable substitution) cannot be evaluated at
  compose-generation time and is silently skipped — no bind entry is emitted for
  that mount.
- `subPath` values that are absolute paths or contain `..` segments are rejected
  with a translation error to prevent directory traversal.

### Intentionally not translated

These are Kubernetes-only concerns with no meaningful local dev equivalent:

- `Service`, `Ingress`, `NetworkPolicy`
- RBAC: `ServiceAccount`, `Role`, `RoleBinding`, `ClusterRole`, `ClusterRoleBinding`
- `HorizontalPodAutoscaler`, `PodDisruptionBudget`
- Init containers (logged as skipped)

---

## Filter Configuration (`kdc-filters.yaml`)

Use a filter file to declaratively control what gets translated. Pass it with `--filters kdc-filters.yaml`.

```yaml
# kdc-filters.yaml
# Glob patterns: * matches any chars including /, ? matches one char.

containers:
  # Drop well-known service-mesh sidecars that have no local equivalent.
  skip:
    - name: istio-proxy
    - name: istio-ingressgateway
    - name: linkerd-proxy
    - name: envoy

  # Swap cloud infrastructure sidecars for local alternatives.
  replace:
    - match:
        # Cloud SQL Auth Proxy → local Postgres
        name: cloud-sql-proxy
      with:
        name: postgres
        image: postgres:16-alpine
        environment:
          POSTGRES_USER: app
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: app
        ports:
          - target: 5432
            published: "5432"

    - match:
        # Any Google Cloud Storage FUSE sidecar → local MinIO
        image: "gcr.io/*/gcsfuse*"
      with:
        name: minio
        image: minio/minio:latest
        environment:
          MINIO_ROOT_USER: minioadmin
          MINIO_ROOT_PASSWORD: minioadmin
        ports:
          - target: 9000
            published: "9000"
          - target: 9001
            published: "9001"

initContainers:
  # Explicitly document init containers that are intentionally dropped.
  skip:
    - name: istio-init
    - name: cloud-sql-proxy-init
    - name: migrate          # DB migration init containers are usually run separately in dev

resources:
  # Skip K8s-only infrastructure resources that have no compose equivalent.
  skip:
    - kind: HorizontalPodAutoscaler
    - kind: PodDisruptionBudget
    - kind: ServiceAccount
    - kind: ClusterRole
    - kind: ClusterRoleBinding
    - kind: Role
    - kind: RoleBinding
    - kind: Ingress
    - kind: NetworkPolicy
```

### Filter rules

- **`containers.skip`** — drop containers by name or image glob. Use this to remove sidecars like `istio-proxy` or `linkerd-proxy` that serve no purpose locally.
- **`containers.replace`** — swap a matched container for a local alternative. Useful for replacing cloud-specific sidecars (e.g. `cloud-sql-proxy` → `postgres`, `gcsfuse` → `minio`).
- **`initContainers.skip`** — document init containers that are intentionally dropped (init containers are never translated; this silences warnings).
- **`resources.skip`** — skip entire Kubernetes resource kinds from translation.

**Glob pattern syntax:** `*` matches any characters including `/`; `?` matches exactly one character.

---

## Override Configuration (`kdc-overrides.yaml`)

Use an overrides file to deep-merge additional Compose configuration onto the generated output. Pass it with `--overrides kdc-overrides.yaml`.

```yaml
# kdc-overrides.yaml
# Deep-merged onto the generated docker-compose.yaml.
# Ports and volumes are APPENDED to generated entries (not replaced).

services:
  web:
    # Expose the web service on localhost port 8080
    ports:
      - target: 80
        published: "8080"
        protocol: tcp
    # Mount local source code for live-reload
    volumes:
      - type: bind
        source: ./src
        target: /usr/share/nginx/html
    # Build from local context instead of pulling an image
    build:
      context: .
      dockerfile: Dockerfile.dev
    environment:
      APP_ENV: development
```

### Merge behaviour

| Field | Behaviour |
|---|---|
| `ports` | **Appended**, deduplicated by target port |
| `volumes` | **Appended**, deduplicated by target mount path |
| `environment` | Merged per-key (override wins on conflict) |
| `image`, `build`, `command`, etc. | Override wins (scalar replacement) |

**Common use cases:**
- Add a `build:` context for live-reload during development
- Bind-mount source code into containers
- Expose additional ports on localhost
- Inject dev-only environment variables (e.g. `APP_ENV: development`)

---

## Generated File Structure

After running `kdc generate`, the following files are produced:

```
.
├── docker-compose.yaml     # Generated compose file
└── .kdc/
    └── envs/
        ├── app-config.env  # From ConfigMap "app-config"
        └── db-secret.env   # From Secret "db-secret"
```

The `.kdc/` directory is managed by kdc. Add it to your `.gitignore` or commit it alongside your `docker-compose.yaml` — both workflows are valid.

---

## Healthchecks

kdc translates Kubernetes readiness and liveness probes into Docker Compose `healthcheck` entries. Both HTTP and TCP probes are converted to **TCP port-open checks** using bash's built-in `/dev/tcp` pseudo-device:

```yaml
healthcheck:
  test: ["CMD-SHELL", "bash -c '(echo >/dev/tcp/localhost/8080) 2>/dev/null'"]
  interval: 10s
  timeout: 5s
  retries: 3
```

**Why `/dev/tcp`?** It is a bash built-in — no `curl`, `wget`, or `nc` is required inside the container. As long as `bash` is available in your image, the healthcheck works.

For local development, verifying that the port is accepting connections is sufficient — the full HTTP response validation that K8s probes provide is not needed on a developer laptop.

**Exec probes** are passed through as-is using the `CMD` form, since they already specify the exact command to run inside the container.

---

## Project Structure

```
cmd/kdc/          CLI entrypoint (cobra)
internal/
  compose/        Compose YAML writer
  envfiles/       .env file generation from ConfigMaps/Secrets
  filter/         Declarative filter engine (skip/replace)
  kustomize/      kustomize build runner
  override/       Compose override deep-merge
  parser/         K8s YAML parser → resource registry
  registry/       Resource registry (indexed storage)
  translator/     K8s → Compose translation logic
pkg/types/        Shared types and constants
testdata/         Test fixtures and golden files
```

---

## Contributing

```bash
# Run tests
go test ./...

# Build
go build ./cmd/kdc
```

Pull requests and issues are welcome. Please open an issue first for significant changes.

---

## License

No license has been chosen for this project yet.

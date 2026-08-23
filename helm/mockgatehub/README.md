# MockGatehub Helm Chart

Deploys [MockGatehub](https://github.com/interledger/mockgatehub) — a mock of the
GateHub API for development and testing — together with a single persistent
[Valkey](https://valkey.io) instance for state and webhook delivery.

## Install

From the published repository, which is what a deployment should use:

```bash
helm repo add mockgatehub https://interledger.github.io/mockgatehub/
helm repo update
helm search repo mockgatehub --versions
helm install mockgatehub mockgatehub/mockgatehub --version <version>
```

Or from a working tree, for developing the chart itself:

```bash
helm dependency build helm/mockgatehub
helm install mockgatehub helm/mockgatehub --set image.tag=<a released tag>
```

`image.tag` is needed for the second form only. Chart.yaml carries `appVersion: 0.0.0`
in git — the real version is stamped by CI at package time — so a working-tree render
otherwise asks for an image tag that does not exist.

**Chart version and application version are always the same number**, and that number is
the image tag. So `mockgatehub 1.15.0` deploys `ghcr.io/interledger/mockgatehub:1.15.0`
and there is no compatibility matrix to consult. See
[Releasing](#releasing) at the bottom.

Point your application at the API Service:

```
GATEHUB_API_BASE_URL=http://mockgatehub.<namespace>.svc.cluster.local:8080
```

## Two listeners, deliberately separate

MockGatehub serves two ports, and this chart gives each its own Service so they
can be exposed independently.

| Service | Port | Serves | Authentication |
|---------|------|--------|----------------|
| `<release>-mockgatehub` | 8080 | GateHub API, iframes, browser-facing card-data and PIN endpoints | HMAC |
| `<release>-mockgatehub-admin` | 8081 | Admin UI (`/ui`) and test-support endpoints (`/admin/*`, `/test-webhook`) | **None** |

The admin listener has no authentication of its own, and several of its
endpoints mutate state — settling a withdrawal, changing a KYC state, simulating
a card transaction. It is meant to be protected by **not being reachable**.

Reach it without exposing it:

```bash
kubectl port-forward svc/<release>-mockgatehub-admin 8081:8081
open http://localhost:8081/ui
```

### Guarding it

Set `networkPolicy.enabled=true`. The application port stays open to any source
unless you narrow `apiIngress`; the admin port is **denied unless** you name a
source in `adminIngress`:

```yaml
networkPolicy:
  enabled: true
  adminIngress:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: ops
```

An empty `adminIngress` emits no rule for the admin port, so the policy denies
it. This is intentional: a NetworkPolicy rule with an empty `from` allows *all*
sources, which is the opposite of what guarding the port means.

Requires a CNI that enforces NetworkPolicy. If yours does not, keep
`adminService.enabled=false` and use `kubectl port-forward`, or place the admin
Service in a namespace nothing else can reach.

`ingress.admin` exists but is off by default. Only enable it behind
authentication you have added yourself.

## State

`valkey.enabled` (default `true`) deploys one Valkey pod with a persistent
volume, using the [groundhog2k/valkey](https://github.com/groundhog2k/helm-charts)
chart — a small non-Bitnami chart that deploys a single workload and a PVC,
which is all a mock needs.

```yaml
valkey:
  enabled: true
  storage:
    requestedSize: 1Gi
    className: ""      # default StorageClass when empty
```

Notes:

- Single instance. `haMode` is off; a sentinel cluster is not useful here.
- The workload uses the `Recreate` strategy, so the ReadWriteOnce volume is
  released before the replacement pod starts.
- The PVC is deleted on `helm uninstall`. Set `valkey.storage.keepPvc=true` to
  keep it.
- No authentication — it is only reachable inside the cluster. Restrict it with
  `valkey.networkPolicy` if that matters to you.

### Alternatives

Use an existing instance:

```yaml
valkey:
  enabled: false
externalRedis:
  url: redis://my-valkey.data.svc.cluster.local:6379
```

Or run with no state at all — MockGatehub falls back to in-memory storage and
**webhook delivery is disabled**:

```yaml
valkey:
  enabled: false
```

The chart refuses this last combination if `config.webhookUrl` is also set,
since those webhooks would never be delivered.

## Webhooks

```yaml
config:
  webhookUrl: http://my-app.default.svc.cluster.local:3000/gatehub-webhooks
credentials:
  webhookSecret: a-shared-secret
```

Deliveries are signed with `X-GH-Webhook-Signature`. `GET /admin/received-webhooks`
on the admin port shows what MockGatehub sent, which is useful when debugging a
consumer that is not reacting.

## Card data links

The card-data and PIN endpoints return an absolute URL for the browser to
follow. It defaults to the in-cluster Service address, which is correct for
server-side callers but not for a browser. If you use the card UI, set:

```yaml
config:
  publicBaseUrl: https://mockgatehub.example.com
```

## Credentials

Rendered into a Secret by default. To manage them yourself, create a Secret with
the keys `MOCKGATEHUB_VALID_CREDENTIALS`, `WEBHOOK_SECRET` and
`MOCKGATEHUB_CARD_DATA_TOKEN_SECRET`, then:

```yaml
credentials:
  existingSecret: my-mockgatehub-secret
```

`MOCKGATEHUB_CARD_DATA_TOKEN_SECRET` may be empty; MockGatehub then generates one
per process. Set it if card-data tokens must survive a restart or be valid across
replicas.

## Values

The commented [`values.yaml`](values.yaml) is the reference. The ones most often
changed:

| Key | Default | Notes |
|-----|---------|-------|
| `image.tag` | chart `appVersion` | The admin listener requires 1.14.0 or later |
| `replicaCount` | `1` | See the note below |
| `config.webhookUrl` | `""` | Where webhooks are delivered |
| `config.publicBaseUrl` | in-cluster Service URL | Needed for browser-followed card links |
| `config.enforceAuthentication` | `true` | HMAC on the application listener only |
| `config.asyncWithdrawals` | `false` | Opt-in; withdrawals stay pending until settled |
| `adminService.enabled` | `true` | Set false to make the admin surface unreachable |
| `networkPolicy.enabled` | `false` | Enable to guard the admin port |
| `valkey.enabled` | `true` | Bundled single persistent instance |
| `valkey.storage.requestedSize` | `1Gi` | Empty means no persistence |

### On `replicaCount`

State lives in Valkey, so more than one replica is safe with one exception: the
test-support webhook sink records deliveries in process memory. A harness
asserting on received webhooks needs a single replica to see them all.

## Development

```bash
make helm-test    # dependency build (no-op while vendored), lint, unit tests, kubeconform
```

Or the individual steps:

```bash
helm dependency build helm/mockgatehub
helm lint helm/mockgatehub
helm unittest helm/mockgatehub
helm template mockgatehub helm/mockgatehub | kubeconform -strict -summary -ignore-missing-schemas -
```

## Releasing

Nothing here is released by hand and there is no chart version to bump.

The `chart` job in `.github/workflows/release.yml` runs on `main` after
semantic-release has cut a version and the image has been pushed. It packages this
directory with `--version` and `--app-version` both set to that version, regenerates
`index.yaml` with `--merge`, and commits the result to the orphan `published` branch,
which GitHub Pages serves.

Three consequences:

- **A chart-only change still needs a releasing commit type.** `fix(helm): ...` or
  `feat(helm): ...` cuts a patch or minor; `chore:` cuts nothing, so a `chore`-titled
  chart fix is never published. See `.releaserc.json` for the mapping.
- **The chart is published only if the image build succeeded.** The job depends on
  `docker`, so an index entry never points at an image that is not there.
- **A published version is immutable.** Re-running the workflow for a version already
  in the index is a deliberate no-op. A wrong chart is fixed by releasing a new version,
  never by editing the `published` branch.

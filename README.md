# `published` — the MockGatehub Helm chart repository

This branch is **not source**. It has no shared history with `main` and holds nothing
but packaged Helm charts and the index that describes them. GitHub Pages serves its
`docs/` folder as a Helm chart repository:

```
https://interledger.github.io/mockgatehub/
```

## Using it

```bash
helm repo add mockgatehub https://interledger.github.io/mockgatehub/
helm repo update
helm search repo mockgatehub --versions
helm install mockgatehub mockgatehub/mockgatehub --version <version>
```

## How it is maintained

Nobody commits here by hand. The `chart` job in `.github/workflows/release.yml` on
`main` runs after a successful release, and:

1. packages `helm/mockgatehub` with `--version` and `--app-version` both set to the
   version semantic-release just cut, into `docs/`;
2. regenerates `docs/index.yaml` with `helm repo index --merge`, so every previously
   published version stays resolvable;
3. commits and pushes here.

Two consequences worth knowing before touching anything:

- **Chart version and appVersion are always equal**, and always equal the image tag.
  `Chart.yaml` on `main` carries `0.0.0`; the real value is stamped at package time and
  never committed back. So there is no chart version to bump by hand.
- **A `.tgz` here is immutable.** Publishing the same version twice is a no-op, not an
  overwrite — `helm repo index --merge` keeps the entry it already has. If a published
  chart is wrong, the fix is a new version, never an edited file on this branch.

## Layout

| Path | What |
|---|---|
| `docs/index.yaml` | the Helm repository index. Generated — never edit it |
| `docs/*.tgz` | one packaged chart per released version |
| `docs/.nojekyll` | stops Pages running Jekyll, which would ignore paths beginning with `_` |
| `docs/index.html` | a landing page, so a human opening the Pages URL gets something |

## Pages configuration

Settings → Pages → Deploy from a branch → branch `published`, folder `/docs`. That
pairing is what makes the URL above work; changing either breaks every consumer's
`helm repo add`.

# devops

How Alexandria reaches the local k3s cluster (context `default`, node
`debian`) at https://alexandria.bmurray.ca.

```
config/deploy/k8s/App.pkl        deploy inputs (schema)
config/deploy/k8s/Manifests.pkl  App -> Namespace, ConfigMaps, PV/PVC, Deployment, Service, Gateway, HTTPRoutes
devops/k3s/alexandria.pkl        this cluster's environment file -> rendered by `make manifests`
devops/flux/alexandria.yaml      OCIRepository + Kustomization in flux-system
deploy/secrets.sh                renders what Flux never owns (`make bootstrap`)
```

## Flow

1. Tag a commit on `main` with `vX.Y.Z` and push the tag. `release.yml`
   refuses tags that are not on `main`, runs the full suite (Go, web,
   Playwright e2e against the real binary), builds the multi-arch image
   `ghcr.io/bmurray/alexandria:X.Y.Z` (linux/amd64 + linux/arm64, no QEMU),
   renders the manifests with `IMAGE_TAG=X.Y.Z make manifests`, and pushes
   them to `oci://ghcr.io/bmurray/alexandria-manifests:X.Y.Z`. Nothing else
   publishes: pushes to `main` only run tests and prove the builds.
2. Flux (`devops/flux/alexandria.yaml`) polls every 5 minutes for the
   newest semver tag of that artifact and applies it with prune and wait.
   Cutting a tag is the deploy.

```
git tag -a v0.1.0 -m "first release" && git push origin v0.1.0
flux --context default reconcile source oci alexandria    # or wait <=5m
kubectl --context default -n alexandria rollout status deploy/alexandria
```

**Rollback**: the artifact for every version stays on ghcr, so re-tag an
older commit with a *newer* version number (`v0.1.2` pointing at the
`v0.1.0` commit) and push; Flux picks the highest semver. The database on
the PV is untouched by rollouts (migrations are forward-only; check
`internal/db` before rolling back across a schema change).

## One-time bootstrap on the cluster

1. **Secrets on your machine** (gitignored, mode 600):

   ```
   mkdir -p ~/.config/secrets/alexandria && chmod 700 ~/.config/secrets/alexandria
   cat > ~/.config/secrets/alexandria/google-oidc.json   # {"client_id": "...", "client_secret": "..."}
   cat > ~/.config/secrets/alexandria/anthropic.json     # {"api_key": "sk-ant-..."}
   ```

2. **Google OAuth client** (Google Cloud console → APIs & Services →
   Credentials → Create OAuth client ID, type *Web application*):
   - Authorised JavaScript origin: `https://alexandria.bmurray.ca`
   - Authorised redirect URI, exactly:
     `https://alexandria.bmurray.ca/api/auth/oidc/google/callback`
   - For local dev add `http://localhost:24380/api/auth/oidc/google/callback` too.
   - Put the client ID in `devops/k3s/alexandria.pkl` (`google.clientId`;
     it is public) and the secret in `google-oidc.json` above.
   - The consent screen must be *External* (or your Workspace) and, while
     in *Testing*, list the test users who may sign in.

3. **Data directory on the node** (the PV is a static `local` volume;
   `/data` is backed up; 65532 is the image's nonroot uid):

   ```
   sudo mkdir -p /data/docker_volumes/alexandria && sudo chown 65532:65532 /data/docker_volumes/alexandria
   ```

4. **Namespace, ServiceAccount, Secret** (never in the artifact):

   ```
   make bootstrap                                   # -> deploy/out/k3s/bootstrap/{namespace,serviceaccount,secret}.yaml
   kubectl config current-context                   # must be `default`
   kubectl --context default apply -f deploy/out/k3s/bootstrap/
   ```

5. **ghcr visibility**: the k3s node pulls `ghcr.io/bmurray/alexandria`
   with its own `registries.yaml` credentials and Flux pulls the manifests
   artifact with the `ghcr-auth` Secret in `flux-system`, so the packages
   may stay private. Making the image package public is simpler for an
   interviewer pulling it by hand.

6. **Flux objects**:

   ```
   kubectl --context default apply -f devops/flux/alexandria.yaml
   flux --context default reconcile source oci alexandria && flux --context default reconcile kustomization alexandria
   ```

   This fails until the first `vX.Y.Z` tag has published an artifact;
   that is expected.

DNS follows from the HTTPRoute hostname (external-dns) and the certificate
from the Gateway's `cert-manager.io/cluster-issuer: letsencrypt-prod`
annotation; the TLS Secret is `alexandria-bmurray-ca-tls`.

## Day two

- **Rotate a secret**: edit the JSON file, `make bootstrap`, re-apply
  `deploy/out/k3s/bootstrap/secret.yaml`, then
  `kubectl --context default -n alexandria rollout restart deploy/alexandria`
  (the projected volume updates on its own, but credentials are read once).
- **Operator overrides** go in the `alexandria-config` ConfigMap
  (`appconfig.pkl`, which `amends "system.pkl"`). The rendered `system.pkl`
  is owned by the artifact; override, don't edit.
- **Spend caps and provider priority** are runtime state in the database
  after first start; admins change them through `PATCH /api/ai/providers/{name}`
  (the `/ai` page). The Pkl values only seed them.
- **Render without CI** (only for a version whose image already exists):

  ```
  IMAGE_TAG=0.1.0 OCI_TAG=0.1.0 make oci-push
  ```

## Checks before trusting a render

```
make manifests
kubectl --context default apply --dry-run=server -f deploy/out/k3s/manifests/   # read-only
grep -c 'kind: ServiceAccount' deploy/out/k3s/manifests/alexandria.yaml           # must be 0
grep -c 'kind: Secret$' deploy/out/k3s/manifests/alexandria.yaml                  # must be 0
```

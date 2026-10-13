#!/usr/bin/env bash
# Render the bootstrap objects Flux never owns: the namespace, the
# ServiceAccount and the Secret (from ~/.config/secrets/alexandria).
# Writes YAML only; never applies (CHAD-DEPLOY: the operator verifies
# kubecontext, then applies).
#
# Secret data keys become files under /var/run/secrets/app in the pod,
# which is what the K8sSecret credentials in the rendered system.pkl read:
#
#   google-client-secret   <- google-oidc.json .client_secret
#   anthropic-api-key      <- anthropic.json   .api_key
#
# Values travel via file descriptors, not argv, so they never show in `ps`.
set -euo pipefail

SECRETS="${SECRETS_DIR:-$HOME/.config/secrets/alexandria}"
GOOGLE="${GOOGLE_OIDC_JSON:-$SECRETS/google-oidc.json}"
ANTHROPIC="${ANTHROPIC_JSON:-$SECRETS/anthropic.json}"
NS="${NAMESPACE:-alexandria}"
APP="${APP:-alexandria}"
OUT="${OUT_DIR:-deploy/out/k3s/bootstrap}"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
command -v kubectl >/dev/null || { echo "kubectl is required (only --dry-run=client is used)" >&2; exit 1; }

missing=0
if [ ! -r "$GOOGLE" ]; then
  echo "missing $GOOGLE" >&2
  echo '  expected JSON: {"client_id": "....apps.googleusercontent.com", "client_secret": "GOCSPX-..."}' >&2
  echo '  (the OAuth client from the Google Cloud console; the same file configs/dev.pkl reads)' >&2
  missing=1
fi
if [ ! -r "$ANTHROPIC" ]; then
  echo "missing $ANTHROPIC" >&2
  echo '  expected JSON: {"api_key": "sk-ant-..."}' >&2
  missing=1
fi
[ "$missing" = 0 ] || exit 1

for pair in "$GOOGLE:client_secret" "$ANTHROPIC:api_key"; do
  f="${pair%%:*}"; k="${pair##*:}"
  v="$(jq -r --arg k "$k" '.[$k] // empty' "$f")"
  [ -n "$v" ] || { echo "$f has no non-empty .$k" >&2; exit 1; }
done

umask 077
mkdir -p "$OUT"

# --dry-run=client never contacts a cluster, so this is safe to run with any
# context active. Keys match config/deploy/k8s/App.pkl (GoogleOidc.secretKey,
# AiApp.anthropicKey).
kubectl create secret generic "${APP}-secrets" \
  --namespace "$NS" \
  --dry-run=client -o yaml \
  --from-file=google-client-secret=<(jq -rj .client_secret "$GOOGLE") \
  --from-file=anthropic-api-key=<(jq -rj .api_key "$ANTHROPIC") \
  > "$OUT/secret.yaml"

# Deploy-owned ServiceAccount; the Deployment only names it.
kubectl create serviceaccount "$APP" --namespace "$NS" \
  --dry-run=client -o yaml > "$OUT/serviceaccount.yaml"

# The namespace is also in the artifact; having it here lets the Secret be
# applied before Flux has run once.
kubectl create namespace "$NS" --dry-run=client -o yaml > "$OUT/namespace.yaml"

echo "wrote $OUT/{namespace,serviceaccount,secret}.yaml (mode 600, gitignored)"
echo "apply with: kubectl --context default apply -f $OUT/"

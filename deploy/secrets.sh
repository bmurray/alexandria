#!/usr/bin/env bash
# Render the bootstrap objects Flux never owns: the namespace, the
# ServiceAccount and the Secret. Writes YAML only; never applies
# (CHAD-DEPLOY: the operator verifies kubecontext, then applies).
#
# Secret data keys become files under /var/run/secrets/app in the pod,
# which is what the K8sSecret credentials in the rendered system.pkl read:
#
#   google-client-id       <- ~/.config/secrets/valsoft.json .client_id
#   google-client-secret   <- ~/.config/secrets/valsoft.json .client_secret
#   openai-api-key         <- ~/.config/secrets/valsoft.json .openai_key
#
# One file holds everything the deployment needs (dev reads the Google part
# of the same file; its OpenAI key is ~/.openai/auth.json). Values travel via
# file descriptors, not argv, so they never show in `ps`.
set -euo pipefail

SECRETS="${SECRETS_JSON:-$HOME/.config/secrets/valsoft.json}"
NS="${NAMESPACE:-alexandria}"
APP="${APP:-alexandria}"
OUT="${OUT_DIR:-deploy/out/k3s/bootstrap}"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
command -v kubectl >/dev/null || { echo "kubectl is required (only --dry-run=client is used)" >&2; exit 1; }

if [ ! -r "$SECRETS" ]; then
  echo "missing $SECRETS" >&2
  echo '  expected JSON: {"client_id": "....apps.googleusercontent.com", "client_secret": "GOCSPX-...", "openai_key": "sk-..."}' >&2
  exit 1
fi
for k in client_id client_secret openai_key; do
  v="$(jq -r --arg k "$k" '.[$k] // empty' "$SECRETS")"
  [ -n "$v" ] || { echo "$SECRETS has no non-empty .$k" >&2; exit 1; }
done

umask 077
mkdir -p "$OUT"

# --dry-run=client never contacts a cluster, so this is safe to run with any
# context active. Keys match config/deploy/k8s/App.pkl (GoogleOidc.idKey,
# GoogleOidc.secretKey, AiApp.openaiKey).
kubectl create secret generic "${APP}-secrets" \
  --namespace "$NS" \
  --dry-run=client -o yaml \
  --from-file=google-client-id=<(jq -rj .client_id "$SECRETS") \
  --from-file=google-client-secret=<(jq -rj .client_secret "$SECRETS") \
  --from-file=openai-api-key=<(jq -rj .openai_key "$SECRETS") \
  > "$OUT/secret.yaml"

# Deploy-owned ServiceAccount; the Deployment only names it.
kubectl create serviceaccount "$APP" --namespace "$NS" \
  --dry-run=client -o yaml > "$OUT/serviceaccount.yaml"

# The namespace is also in the artifact; having it here lets the Secret be
# applied before Flux has run once.
kubectl create namespace "$NS" --dry-run=client -o yaml > "$OUT/namespace.yaml"

echo "wrote $OUT/{namespace,serviceaccount,secret}.yaml (mode 600, gitignored)"
echo "apply with: kubectl --context default apply -f $OUT/"

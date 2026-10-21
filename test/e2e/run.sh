#!/usr/bin/env bash
# End-to-end test of the provider against the mock Hostinger API
# (test/mockapi), in a kind cluster:
#   - installs the CRDs and runs the controller out of cluster
#   - for the cluster-scoped and the namespaced APIs: creates an SSHKey, a
#     PostInstallScript, a DNS Record and a VPS that references both, waits
#     until all are Ready, updates the script and the VPS hostname, then
#     deletes everything and checks the mock is empty again
#
# Needs docker, kind, kubectl, jq, curl and go. Takes about 5 minutes.
#   KEEP=1  keep the cluster and processes afterwards
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=$(mktemp -d)
cluster=provider-hostinger-e2e
api=127.0.0.1:${MOCK_PORT:-8085}
token=e2e-token
export KUBECONFIG="$work/kubeconfig"

log() { printf '\n\033[1m== %s\033[0m\n' "$*"; }
fail() {
  printf '\033[31mFAIL: %s\033[0m\n' "$*"
  echo "--- provider log (tail)"; tail -40 "$work/provider.log" || true
  exit 1
}

cleanup() {
  for pid in ${provider_pid:-} ${mock_pid:-}; do kill "$pid" 2>/dev/null || true; done
  if [[ "${KEEP:-}" != 1 ]]; then
    kind delete cluster --name "$cluster" >/dev/null 2>&1 || true
    rm -rf "$work"
  else
    echo "kept: KUBECONFIG=$work/kubeconfig, logs in $work"
  fi
}
trap cleanup EXIT

mock() { curl -sf -H "Authorization: Bearer $token" "http://$api$1"; }

# ready <resource/name> [namespace] waits for Ready=True and Synced=True.
ready() {
  local ns=()
  [[ -n "${2:-}" ]] && ns=(-n "$2")
  kubectl wait "$1" "${ns[@]}" --for=condition=Ready --timeout=180s >/dev/null ||
    fail "$1 not ready: $(kubectl get "$1" "${ns[@]}" -o jsonpath='{.status.conditions}')"
  kubectl wait "$1" "${ns[@]}" --for=condition=Synced --timeout=60s >/dev/null
}

log "building"
(cd "$root" && go build -o "$work/provider" ./cmd/provider && go build -o "$work/mockapi" ./test/mockapi)

log "cluster"
kind create cluster --name "$cluster" --wait 180s
kubectl apply --server-side -f "$root/package/crds" >/dev/null
kubectl wait crd --all --for=condition=Established --timeout=60s >/dev/null

log "mock API and controller"
"$work/mockapi" -addr "$api" -token "$token" > "$work/mock.log" 2>&1 &
mock_pid=$!
"$work/provider" --debug --poll=15s > "$work/provider.log" 2>&1 &
provider_pid=$!
until mock /api/vps/v1/templates >/dev/null; do sleep 1; done

kubectl create namespace crossplane-system
kubectl -n crossplane-system create secret generic hostinger-creds --from-literal=credentials="{\"api_token\": \"$token\"}"
kubectl create namespace team-a
kubectl -n team-a create secret generic hostinger-creds --from-literal=credentials="$token"
kubectl apply -f - <<EOF
apiVersion: hostinger.crossplane.io/v1beta1
kind: ProviderConfig
metadata: {name: default}
spec:
  endpoint: http://$api
  credentials:
    source: Secret
    secretRef: {name: hostinger-creds, namespace: crossplane-system, key: credentials}
---
apiVersion: hostinger.m.crossplane.io/v1beta1
kind: ProviderConfig
metadata: {name: default, namespace: team-a}
spec:
  endpoint: http://$api
  credentials:
    source: Secret
    # A namespaced ProviderConfig always reads the Secret from the managed
    # resource's namespace; the field is required by the schema.
    secretRef: {name: hostinger-creds, namespace: team-a, key: credentials}
EOF

# run_scope <cluster|namespaced>
run_scope() {
  local scope=$1 group ns=() nsarg="" nsfield="" pcref secretns
  if [[ $scope == cluster ]]; then
    group=hostinger.crossplane.io pcref="name: default" secretns=crossplane-system
  else
    group=hostinger.m.crossplane.io pcref="kind: ProviderConfig, name: default" secretns=team-a
    ns=(-n team-a) nsarg=team-a nsfield=", namespace: team-a"
  fi
  # Cluster-scoped resources name the Secret's namespace; namespaced ones
  # can only use Secrets in their own namespace.
  local pwref="{name: root-password, key: password, namespace: crossplane-system}"
  [[ $scope == namespaced ]] && pwref="{name: root-password, key: password}"

  log "$scope: create"
  kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata: {name: root-password, namespace: $secretns}
stringData: {password: correct-horse-battery}
---
apiVersion: vps.$group/v1alpha1
kind: SSHKey
metadata: {name: laptop$nsfield, labels: {owner: e2e}}
spec:
  forProvider:
    name: laptop-$scope
    key: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE2E2E2E2E2E2E2E2E2E2E2E2E2E2E2E2E2E2E2E2E2E e2e@example.com
  providerConfigRef: {$pcref}
---
apiVersion: vps.$group/v1alpha1
kind: PostInstallScript
metadata: {name: setup$nsfield}
spec:
  forProvider:
    name: setup-$scope
    content: "#!/bin/sh\necho v1\n"
  providerConfigRef: {$pcref}
---
apiVersion: dns.$group/v1alpha1
kind: Record
metadata: {name: www$nsfield}
spec:
  forProvider: {zone: example.com, name: www-$scope, type: A, value: 203.0.113.10, ttl: 300}
  providerConfigRef: {$pcref}
---
apiVersion: vps.$group/v1alpha1
kind: VPS
metadata: {name: web$nsfield}
spec:
  forProvider:
    plan: hostingercom-vps-kvm2-usd-1m
    dataCenterId: 13
    templateId: 1002
    hostname: web-$scope.example.com
    passwordSecretRef: $pwref
    postInstallScriptIdRef: {name: setup}
    sshKeyIdsSelector: {matchLabels: {owner: e2e}}
  providerConfigRef: {$pcref}
EOF
  for r in "sshkey.vps.$group/laptop" "postinstallscript.vps.$group/setup" "record.dns.$group/www" "vps.vps.$group/web"; do
    ready "$r" "$nsarg"
  done

  local key_id script_id vps_id
  key_id=$(kubectl get "sshkey.vps.$group/laptop" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}')
  script_id=$(kubectl get "postinstallscript.vps.$group/setup" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}')
  vps_id=$(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}')
  [[ $(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.spec.forProvider.postInstallScriptId}') == "$script_id" ]] || fail "postInstallScriptId not resolved"
  [[ $(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.spec.forProvider.sshKeyIds[0]}') == "$key_id" ]] || fail "sshKeyIds not resolved"
  [[ -n $(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.status.atProvider.ipv4Address}') ]] || fail "no ipv4Address"
  mock "/api/vps/v1/virtual-machines/$vps_id/public-keys" | jq -e --argjson id "$key_id" '.data[] | select(.id == $id)' >/dev/null || fail "SSH key not attached"
  [[ $(kubectl get "record.dns.$group/www" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}') == "www-$scope|A|203.0.113.10" ]] || fail "record external name"
  echo "VPS $vps_id with script $script_id and key $key_id; record www-$scope"

  log "$scope: update"
  kubectl patch "postinstallscript.vps.$group/setup" "${ns[@]}" --type merge -p '{"spec":{"forProvider":{"content":"#!/bin/sh\necho v2\n"}}}' >/dev/null
  kubectl patch "vps.vps.$group/web" "${ns[@]}" --type merge -p "{\"spec\":{\"forProvider\":{\"hostname\":\"renamed-$scope.example.com\"}}}" >/dev/null
  local deadline=$((SECONDS + 120))
  until mock "/api/vps/v1/post-install-scripts/$script_id" | jq -e '.content | contains("v2")' >/dev/null &&
    mock "/api/vps/v1/virtual-machines/$vps_id" | jq -e --arg h "renamed-$scope.example.com" '.hostname == $h' >/dev/null; do
    ((SECONDS < deadline)) || fail "updates not applied"
    sleep 3
  done
  echo "script content and hostname updated"

  log "$scope: delete"
  kubectl delete "vps.vps.$group/web" "${ns[@]}" --wait=false >/dev/null
  kubectl wait "vps.vps.$group/web" "${ns[@]}" --for=delete --timeout=180s >/dev/null || fail "VPS not deleted"
  kubectl delete "sshkey.vps.$group/laptop" "postinstallscript.vps.$group/setup" "record.dns.$group/www" "${ns[@]}" --wait=false >/dev/null
  for r in "sshkey.vps.$group/laptop" "postinstallscript.vps.$group/setup" "record.dns.$group/www"; do
    kubectl wait "$r" "${ns[@]}" --for=delete --timeout=180s >/dev/null || fail "$r not deleted: $(kubectl get "$r" "${ns[@]}" -o jsonpath='{.status.conditions}')"
  done
  [[ $(mock /api/vps/v1/public-keys | jq '.data | length') == 0 ]] || fail "SSH key left behind"
  [[ $(mock /api/vps/v1/virtual-machines | jq 'length') == 0 ]] || fail "VPS left behind"
  mock "/api/vps/v1/post-install-scripts/$script_id" >/dev/null 2>&1 && fail "script left behind"
  [[ $(mock /api/dns/v1/zones/example.com | jq 'length') == 0 ]] || fail "DNS record left behind"
  echo "everything deleted"
}

run_scope cluster
run_scope namespaced

log "PASS"

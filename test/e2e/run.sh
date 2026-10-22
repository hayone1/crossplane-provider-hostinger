#!/usr/bin/env bash
# End-to-end test of the provider against the mock Hostinger API
# (test/mockapi), in a kind cluster:
#   - installs the CRDs and runs the controller out of cluster
#   - for the cluster-scoped and the namespaced APIs: creates an SSHKey, a
#     PostInstallScript, a DNS Record and a VPS that references both, waits
#     until all are Ready, updates the script and the VPS hostname, then
#     deletes everything and checks the mock is empty again
#
# Needs docker, kind, kubectl, jq, curl, ssh-keygen and go. Takes about 5
# minutes.
#   KEEP=1  keep the cluster and processes afterwards
#   POLL=1m poll interval of the controller (default 15s); the real API is
#           rate limited
#   REAL=1  use the real Hostinger API instead of the mock. Needs
#           HOSTINGER_API_KEY (read from .env if present) and ZONE, a DNS
#           zone in the account. Only SSHKey, PostInstallScript and Record
#           are tested unless VPS_SCOPE=cluster|namespaced, which buys ONE
#           VPS ($PLAN, default KVM 1 monthly) in that scope and cancels it
#           at the end. With VPS_ID=<id> that VPS is imported instead of
#           bought, and still cancelled at the end.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=$(mktemp -d)
cluster=provider-hostinger-e2e
export KUBECONFIG="$work/kubeconfig"
if [[ "${REAL:-}" == 1 ]]; then
  [[ -f "$root/.env" ]] && { set -a; . "$root/.env"; set +a; }
  base=https://developers.hostinger.com
  token=${HOSTINGER_API_KEY:?HOSTINGER_API_KEY not set}
  zone=${ZONE:?ZONE not set}
  plan=${PLAN:-hostingercom-vps-kvm1-usd-1m}
  data_center=${DATA_CENTER:-24} template=${TEMPLATE:-1077}
  vps_scope=${VPS_SCOPE:-}
  run=$(date +%s)
else
  base=http://127.0.0.1:${MOCK_PORT:-8085}
  token=e2e-token zone=example.com plan=hostingercom-vps-kvm2-usd-1m
  data_center=13 template=1002 vps_scope=all run=e2e
fi

log() { printf '\n\033[1m== %s\033[0m\n' "$*"; }
fail() {
  printf '\033[31mFAIL: %s\033[0m\n' "$*"
  echo "--- provider log (tail)"; tail -40 "$work/provider.log" || true
  exit 1
}

cleanup() {
  # On the real API, delete whatever is left while the controller still runs
  # so a failed run does not leave paid or visible resources behind.
  if [[ "${REAL:-}" == 1 && "${KEEP:-}" != 1 && -n "${provider_pid:-}" ]]; then
    kubectl delete managed --all -A --timeout=600s >/dev/null 2>&1 ||
      echo "WARNING: cleanup incomplete, check the Hostinger account"
  fi
  for pid in ${provider_pid:-} ${mock_pid:-}; do kill "$pid" 2>/dev/null || true; done
  if [[ "${KEEP:-}" != 1 ]]; then
    kind delete cluster --name "$cluster" >/dev/null 2>&1 || true
    rm -rf "$work"
  else
    echo "kept: KUBECONFIG=$work/kubeconfig, logs in $work"
  fi
}
trap cleanup EXIT

api() { curl -sf -H "Authorization: Bearer $token" "$base$1"; }

# ready <resource/name> [namespace] [timeout] waits for Ready=True and Synced=True.
ready() {
  local ns=()
  [[ -n "${2:-}" ]] && ns=(-n "$2")
  kubectl wait "$1" "${ns[@]}" --for=condition=Ready --timeout="${3:-180s}" >/dev/null ||
    fail "$1 not ready: $(kubectl get "$1" "${ns[@]}" -o jsonpath='{.status.conditions}')"
  kubectl wait "$1" "${ns[@]}" --for=condition=Synced --timeout=60s >/dev/null
}

log "building"
(cd "$root" && go build -o "$work/provider" ./cmd/provider && go build -o "$work/mockapi" ./test/mockapi)
ssh-keygen -q -t ed25519 -N '' -C "e2e-$run@example.com" -f "$work/id_e2e"
ssh-keygen -q -t ed25519 -N '' -C "e2e-$run-2@example.com" -f "$work/id_e2e2"
pubkey=$(cat "$work/id_e2e.pub") pubkey2=$(cat "$work/id_e2e2.pub")
# The password is also used for the control panel; Hostinger allows only the
# symbols # + ? @ in both.
password="E2e#$(openssl rand -hex 12)@9"

log "cluster"
kind create cluster --name "$cluster" --wait 180s
kubectl apply --server-side -f "$root/package/crds" >/dev/null
kubectl wait crd --all --for=condition=Established --timeout=60s >/dev/null

log "API and controller"
if [[ "${REAL:-}" != 1 ]]; then
  "$work/mockapi" -addr "${base#http://}" -token "$token" > "$work/mock.log" 2>&1 &
  mock_pid=$!
fi
"$work/provider" --debug --poll="${POLL:-15s}" > "$work/provider.log" 2>&1 &
provider_pid=$!
until api /api/vps/v1/templates >/dev/null; do sleep 1; done
scripts_allowed=""
api /api/vps/v1/post-install-scripts >/dev/null && scripts_allowed=1

kubectl create namespace crossplane-system
kubectl -n crossplane-system create secret generic hostinger-creds --from-literal=credentials="{\"api_token\": \"$token\"}"
kubectl create namespace team-a
kubectl -n team-a create secret generic hostinger-creds --from-literal=credentials="$token"
kubectl apply -f - <<EOF
apiVersion: hostinger.crossplane.io/v1beta1
kind: ProviderConfig
metadata: {name: default}
spec:
  endpoint: $base
  credentials:
    source: Secret
    secretRef: {name: hostinger-creds, namespace: crossplane-system, key: credentials}
---
apiVersion: hostinger.m.crossplane.io/v1beta1
kind: ProviderConfig
metadata: {name: default, namespace: team-a}
spec:
  endpoint: $base
  credentials:
    source: Secret
    # A namespaced ProviderConfig always reads the Secret from the managed
    # resource's namespace; the field is required by the schema.
    secretRef: {name: hostinger-creds, namespace: team-a, key: credentials}
EOF

# has_key <vps id> <public key> checks the key is installed on the VPS.
has_key() {
  api "/api/vps/v1/virtual-machines/$1/ssh-keys" |
    jq -e --arg k "$(cut -d' ' -f1,2 <<<"$2")" 'any(.[]; (.key | split(" ")[0:2] | join(" ")) == $k)' >/dev/null
}

# delete_vps <group> [-n namespace]
# Deleting a VPS turns off its subscription's auto-renewal; the API can't
# cancel it any more, so the VPS stays until the end of the billing period.
delete_vps() {
  local group=$1 id sub; shift
  id=$(kubectl get "vps.vps.$group/web" "$@" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}')
  sub=$(api "/api/vps/v1/virtual-machines/$id" | jq -r .subscription_id)
  kubectl delete "vps.vps.$group/web" "$@" --wait=false >/dev/null
  kubectl wait "vps.vps.$group/web" "$@" --for=delete --timeout=600s >/dev/null || fail "VPS not deleted"
  api /api/billing/v1/subscriptions | jq -e --arg s "$sub" 'any(.[]; .id == $s and .is_auto_renewed == false)' >/dev/null ||
    fail "subscription $sub of VPS $id still renews"
}

# run_scope <cluster|namespaced>
run_scope() {
  local scope=$1 group ns=() nsarg="" nsfield="" pcref secretns with_vps=""
  if [[ $scope == cluster ]]; then
    group=hostinger.crossplane.io pcref="name: default" secretns=crossplane-system
  else
    group=hostinger.m.crossplane.io pcref="kind: ProviderConfig, name: default" secretns=team-a
    ns=(-n team-a) nsarg=team-a nsfield=", namespace: team-a"
  fi
  [[ $vps_scope == all || $vps_scope == "$scope" ]] && with_vps=1
  # Cluster-scoped resources name the Secret's namespace; namespaced ones
  # can only use Secrets in their own namespace.
  local pwref="{name: root-password, key: password, namespace: crossplane-system}"
  [[ $scope == namespaced ]] && pwref="{name: root-password, key: password}"
  # Hostinger refuses post-install scripts until the account has a VPS. In
  # such an account the VPS must not wait for the script.
  local script_ref="    postInstallScriptIdRef: {name: setup}"
  [[ -z $scripts_allowed ]] && script_ref="    # no postInstallScriptIdRef: the account has no VPS yet"
  local vps_annotations=""
  [[ -n "${VPS_ID:-}" ]] && vps_annotations=", annotations: {crossplane.io/external-name: \"$VPS_ID\"}"
  local rec="e2e-$run-$scope" resources=("sshkey.vps.$group/laptop" "sshkey.vps.$group/laptop2" "postinstallscript.vps.$group/setup" "record.dns.$group/www")

  log "$scope: create"
  {
    cat <<EOF
apiVersion: v1
kind: Secret
metadata: {name: root-password, namespace: $secretns}
stringData: {password: "$password"}
---
apiVersion: vps.$group/v1alpha1
kind: SSHKey
metadata: {name: laptop$nsfield, labels: {owner: e2e}}
spec:
  forProvider:
    name: e2e-$run-$scope
    key: $pubkey
  providerConfigRef: {$pcref}
---
apiVersion: vps.$group/v1alpha1
kind: SSHKey
metadata: {name: laptop2$nsfield}
spec:
  forProvider:
    name: e2e-$run-$scope-2
    key: $pubkey2
  providerConfigRef: {$pcref}
---
apiVersion: vps.$group/v1alpha1
kind: PostInstallScript
metadata: {name: setup$nsfield}
spec:
  forProvider:
    name: e2e-$run-$scope
    content: "#!/bin/sh\necho v1\n"
  providerConfigRef: {$pcref}
---
apiVersion: dns.$group/v1alpha1
kind: Record
metadata: {name: www$nsfield}
spec:
  forProvider: {zone: $zone, name: $rec, type: A, value: 203.0.113.10, ttl: 300}
  providerConfigRef: {$pcref}
EOF
    if [[ -n $with_vps ]]; then cat <<EOF
---
apiVersion: vps.$group/v1alpha1
kind: VPS
metadata: {name: web$nsfield$vps_annotations}
spec:
  forProvider:
    plan: $plan
    dataCenterId: $data_center
    templateId: $template
    hostname: web-$scope.$zone
    passwordSecretRef: $pwref
$script_ref
    sshKeyIdsSelector: {matchLabels: {owner: e2e}}
  providerConfigRef: {$pcref}
EOF
    fi
  } | kubectl apply -f -
  [[ -n $with_vps ]] && ready "vps.vps.$group/web" "$nsarg" 1200s
  for r in "${resources[@]}"; do ready "$r" "$nsarg" 600s; done

  local key_id key2_id script_id vps_id=""
  key_id=$(kubectl get "sshkey.vps.$group/laptop" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}')
  key2_id=$(kubectl get "sshkey.vps.$group/laptop2" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}')
  script_id=$(kubectl get "postinstallscript.vps.$group/setup" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}')
  [[ $(kubectl get "record.dns.$group/www" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}') == "$rec|A|203.0.113.10" ]] || fail "record external name"
  api "/api/dns/v1/zones/$zone" | jq -e --arg n "$rec" '.[] | select(.name == $n and .type == "A")' >/dev/null || fail "record not in zone"
  if [[ -n $with_vps ]]; then
    vps_id=$(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}')
    if [[ -n $scripts_allowed ]]; then
      [[ $(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.spec.forProvider.postInstallScriptId}') == "$script_id" ]] || fail "postInstallScriptId not resolved"
    fi
    [[ $(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.spec.forProvider.sshKeyIds[0]}') == "$key_id" ]] || fail "sshKeyIds not resolved"
    [[ -n $(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.status.atProvider.ipv4Address}') ]] || fail "no ipv4Address"
    # An imported VPS is taken as it is; its keys are attached on update.
    [[ -n "${VPS_ID:-}" ]] || has_key "$vps_id" "$pubkey" || fail "SSH key not installed"
  fi
  echo "VPS ${vps_id:-skipped}, script $script_id, key $key_id, record $rec"

  log "$scope: update"
  kubectl patch "postinstallscript.vps.$group/setup" "${ns[@]}" --type merge -p '{"spec":{"forProvider":{"content":"#!/bin/sh\necho v2\n"}}}' >/dev/null
  [[ -n $with_vps ]] && kubectl patch "vps.vps.$group/web" "${ns[@]}" --type merge \
    -p "{\"spec\":{\"forProvider\":{\"hostname\":\"renamed-$scope.$zone\",\"sshKeyIds\":[$key_id,$key2_id]}}}" >/dev/null
  local deadline=$((SECONDS + 300))
  until api "/api/vps/v1/post-install-scripts/$script_id" | jq -e '.content | contains("v2")' >/dev/null &&
    { [[ -z $with_vps ]] || { api "/api/vps/v1/virtual-machines/$vps_id" | jq -e --arg h "renamed-$scope.$zone" '.hostname == $h' >/dev/null &&
      has_key "$vps_id" "$pubkey2"; }; }; do
    ((SECONDS < deadline)) || fail "updates not applied"
    sleep 5
  done
  if [[ -n $with_vps && "${REAL:-}" == 1 ]]; then
    local ip
    ip=$(kubectl get "vps.vps.$group/web" "${ns[@]}" -o jsonpath='{.status.atProvider.ipv4Address}')
    until ssh -i "$work/id_e2e2" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
      -o ConnectTimeout=10 "root@$ip" true 2>/dev/null; do
      ((SECONDS < deadline)) || fail "cannot log in to $ip with the added SSH key"
      sleep 10
    done
  fi
  echo "updates applied"

  log "$scope: delete"
  # On the real API the VPS is kept until both scopes ran (see delete_vps).
  [[ -n $with_vps && "${REAL:-}" != 1 ]] && delete_vps "$group" "${ns[@]}"
  kubectl delete "${resources[@]}" "${ns[@]}" --wait=false >/dev/null
  for r in "${resources[@]}"; do
    kubectl wait "$r" "${ns[@]}" --for=delete --timeout=180s >/dev/null || fail "$r not deleted: $(kubectl get "$r" "${ns[@]}" -o jsonpath='{.status.conditions}')"
  done
  api /api/vps/v1/public-keys | jq -e --argjson id "$key_id" '.data[] | select(.id == $id)' >/dev/null && fail "SSH key left behind"
  api "/api/vps/v1/post-install-scripts/$script_id" >/dev/null 2>&1 && fail "script left behind"
  api "/api/dns/v1/zones/$zone" | jq -e --arg n "$rec" '.[]? | select(.name == $n)' >/dev/null && fail "DNS record left behind"
  echo "everything deleted"
}

run_scope cluster
run_scope namespaced
if [[ "${REAL:-}" == 1 && -n $vps_scope ]]; then
  log "delete VPS"
  if [[ $vps_scope == cluster ]]; then delete_vps hostinger.crossplane.io; else delete_vps hostinger.m.crossplane.io -n team-a; fi
  echo "VPS deleted"
fi

log "PASS"

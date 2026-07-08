#!/usr/bin/env bash
# Configuration commune aux scripts de stress.
# Résout l'IP de l'ingress (la cible de charge) dans cet ordre :
#   1. Variable d'env TARGET_IP (override manuel)
#   2. terraform output -raw ingress_public_ip
#   3. ansible/inventory/hosts.ini  (ligne "ingress ansible_host=...")
#
# Usage : source ./env.sh   (fait par les autres scripts automatiquement)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

resolve_target_ip() {
  # 1. Override explicite
  if [[ -n "${TARGET_IP:-}" ]]; then
    echo "$TARGET_IP"
    return 0
  fi

  # 2. Terraform state
  local ip
  ip="$(terraform -chdir="$REPO_ROOT/terraform" output -raw ingress_public_ip 2>/dev/null || true)"
  if [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "$ip"
    return 0
  fi

  # 3. Inventaire Ansible : "ingress ansible_host=<ip> ..."
  local inv="$REPO_ROOT/ansible/inventory/hosts.ini"
  if [[ -f "$inv" ]]; then
    ip="$(grep -E '^ingress[[:space:]]' "$inv" \
          | grep -oE 'ansible_host=[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' \
          | head -1 | cut -d= -f2 || true)"
    if [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "$ip"
      return 0
    fi
  fi

  echo "ERROR: impossible de trouver l'IP de l'ingress." >&2
  echo "       Renseigne-la à la main :  export TARGET_IP=1.2.3.4" >&2
  return 1
}

TARGET_IP="$(resolve_target_ip)"
# podinfo répond sans Host particulier (ingress par défaut) => http://<ip>/ suffit.
BASE_URL="http://${TARGET_IP}"

# Lance un binaire, en le récupérant via nix s'il n'est pas sur le PATH.
# (jamais brew : on reste sur nix)
run_tool() {
  local bin="$1"; shift
  if command -v "$bin" >/dev/null 2>&1; then
    "$bin" "$@"
  else
    echo "→ '$bin' absent du PATH, lancement via nix (nixpkgs#$bin)…" >&2
    nix run "nixpkgs#$bin" -- "$@"
  fi
}

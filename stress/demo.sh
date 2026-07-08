#!/usr/bin/env bash
# ============================================================================
#  demo.sh — boîte à outils de stress podinfo pour la présentation Cluster69
# ============================================================================
#  Sous-commandes :
#    smoke      Vérifie que podinfo répond + affiche version/pod
#    watch      Affiche en direct quels pods servent (preuve visuelle du scale)
#    autoscale  Montée en charge progressive -> déclenche le HPA (2 -> 6 pods)
#    spike      Pic brutal -> montre la réactivité HPA + latence
#    latency    Sature via /delay -> latence p95/p99 qui explose (Grafana)
#    chaos      Injecte des erreurs 5xx -> allume les dashboards / alertes
#    survivor   CHAOS MONKEY : tue des pods en boucle + mesure la dispo en direct
#
#  Intensité réglable :  VUS=60 ./demo.sh autoscale
#                        KILL_EVERY=3 DURATION=90 ./demo.sh survivor
#  Cible forcée       :  TARGET_IP=1.2.3.4 ./demo.sh smoke
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "$SCRIPT_DIR/env.sh"

banner() { printf '\n\033[1;36m== %s ==\033[0m\n' "$1"; }
info()   { printf '\033[0;90m%s\033[0m\n' "$1"; }

k6_run() {
  banner "$1"; shift
  info "Cible : $BASE_URL   |   VUS=${VUS:-défaut}"
  BASE_URL="$BASE_URL" VUS="${VUS:-}" DELAY="${DELAY:-}" \
    run_tool k6 run "$@"
}

cmd_smoke() {
  banner "Smoke test — podinfo est-il vivant ?"
  info "Cible : $BASE_URL"
  echo "→ /version :"
  curl -fsS "$BASE_URL/version" && echo
  echo "→ /api/info (extrait) :"
  curl -fsS "$BASE_URL/api/info" | grep -oE '"(hostname|version|num_goroutine)":[^,}]*' || true
  echo
  info "OK si tu vois une version et un hostname ci-dessus."
}

cmd_watch() {
  banner "Watch — répartition de la charge sur les pods"
  info "Ctrl-C pour arrêter. En parallèle, ouvre k9s sur le namespace 'podinfo'."
  info "Chaque ligne = 'nb_requêtes  nom_du_pod' sur les 30 derniers hits."
  while true; do
    for _ in $(seq 1 30); do
      curl -fsS "$BASE_URL/api/info" 2>/dev/null \
        | grep -oE '"hostname": *"[^"]*"' | cut -d'"' -f4
    done | sort | uniq -c | sort -rn
    echo "----- $(date +%H:%M:%S) -----"
    sleep 2
  done
}

cmd_autoscale() { k6_run "Autoscale — montée progressive (déclenche le HPA)" "$SCRIPT_DIR/k6/ramp.js"; }
cmd_spike()     { k6_run "Spike — pic brutal"                                  "$SCRIPT_DIR/k6/spike.js"; }
cmd_latency()   { k6_run "Latency — saturation via /delay"                     "$SCRIPT_DIR/k6/latency.js"; }

cmd_chaos() {
  banner "Chaos — injection d'erreurs 5xx"
  info "Cible : $BASE_URL — on demande à podinfo de renvoyer des 500 pendant ${CHAOS_SECONDS:-30}s."
  info "Objectif : faire monter le taux d'erreur sur les dashboards / déclencher des alertes."
  local end=$(( $(date +%s) + ${CHAOS_SECONDS:-30} ))
  local n=0
  while [[ $(date +%s) -lt $end ]]; do
    # /status/500 => podinfo répond volontairement en 500. On mixe un peu de 200.
    curl -fsS -o /dev/null "$BASE_URL/status/500" 2>/dev/null || true
    curl -fsS -o /dev/null "$BASE_URL/status/503" 2>/dev/null || true
    curl -fsS -o /dev/null "$BASE_URL/api/info"   2>/dev/null || true
    n=$((n+3))
    printf '\r  %d requêtes envoyées…' "$n"
  done
  echo; info "Terminé. Va voir le taux d'erreur grimper dans Grafana."
}

cmd_survivor() {
  banner "Survivor — Chaos Monkey : je tue des pods, l'appli survit"
  info "Cible : $BASE_URL"
  info "Conseil : lance d'abord './demo.sh spike' pour monter à 6 pods (plus résilient),"
  info "          et garde k9s ouvert sur le ns 'podinfo' — mate la colonne RESTARTS."
  local duration=${DURATION:-60}
  local kill_every=${KILL_EVERY:-5}
  info "Durée : ${duration}s   |   un pod tué toutes les ${kill_every}s"
  sleep 2

  local end=$(( $(date +%s) + duration ))
  local last_kill=0 total=0 ok=0 kills=0 code now avail c
  while [[ $(date +%s) -lt $end ]]; do
    # Sonde de disponibilité (ne compte PAS la requête /panic elle-même)
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$BASE_URL/api/info" 2>/dev/null || echo 000)
    total=$((total+1))
    [[ "$code" == "200" ]] && ok=$((ok+1))

    # Meurtre périodique d'un pod (au hasard, via l'ingress)
    now=$(date +%s)
    if (( now - last_kill >= kill_every )); then
      curl -s -o /dev/null --max-time 2 "$BASE_URL/panic" 2>/dev/null || true
      kills=$((kills+1)); last_kill=$now
      printf '\r\033[K\033[0;31m ☠️  pod #%d tué — Kubernetes le relance…\033[0m\n' "$kills"
    fi

    # Ligne de dispo live (verte >=99%, jaune >=95%, rouge sinon)
    avail=$(awk -v ok="$ok" -v t="$total" 'BEGIN{ if(t>0) printf "%.1f",100*ok/t; else printf "0.0" }')
    if   awk -v a="$avail" 'BEGIN{exit !(a>=99)}'; then c='\033[0;32m'
    elif awk -v a="$avail" 'BEGIN{exit !(a>=95)}'; then c='\033[0;33m'
    else c='\033[0;31m'; fi
    printf '\r  dispo: '"$c"'%s%%\033[0m  |  requêtes: %d  |  pods tués: %d   ' "$avail" "$total" "$kills"
  done
  printf '\n'
  info "Bilan : $ok/$total requêtes OK malgré $kills pods tués. Voilà la résilience Kubernetes. 🦖"
}

usage() {
  grep -E '^#( |    )' "$0" | sed 's/^# \{0,1\}//'
}

main() {
  local cmd="${1:-help}"
  case "$cmd" in
    smoke)     cmd_smoke ;;
    watch)     cmd_watch ;;
    autoscale) cmd_autoscale ;;
    spike)     cmd_spike ;;
    latency)   cmd_latency ;;
    chaos)     cmd_chaos ;;
    survivor)  cmd_survivor ;;
    help|-h|--help) usage ;;
    *) echo "Commande inconnue : $cmd"; echo; usage; exit 1 ;;
  esac
}

main "$@"

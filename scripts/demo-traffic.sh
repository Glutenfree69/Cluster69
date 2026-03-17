#!/usr/bin/env bash
# demo-traffic.sh — Generate varied HTTP traffic against podinfo
# Usage: ./scripts/demo-traffic.sh [BASE_URL]
# Example: ./scripts/demo-traffic.sh http://44.200.10.5

set -euo pipefail

BASE_URL="${1:-http://15.237.119.243/}"
ROUNDS="${2:-5}"

echo "=== KubeQuest Demo Traffic Generator ==="
echo "Target : ${BASE_URL}"
echo "Rounds : ${ROUNDS}"
echo ""

# Routes with expected behavior
ROUTES=(
  "GET  /           200  normal"
  "GET  /healthz    200  healthcheck"
  "GET  /version    200  version-info"
  "GET  /status/500 500  server-error"
  "GET  /status/503 503  service-unavailable"
  "GET  /delay/2    200  slow-response"
  "POST /echo       200  echo-post"
)

request() {
  local method="$1" path="$2" label="$3"
  local url="${BASE_URL}${path}"
  local status

  if [[ "${method}" == "POST" ]]; then
    status=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
      -H "Content-Type: application/json" \
      -d '{"message":"hello from demo-traffic"}' \
      --max-time 10 "${url}" 2>/dev/null) || status="ERR"
  else
    status=$(curl -s -o /dev/null -w "%{http_code}" \
      --max-time 10 "${url}" 2>/dev/null) || status="ERR"
  fi

  printf "  %-6s %-15s -> %s  (%s)\n" "${method}" "${path}" "${status}" "${label}"
}

for ((r = 1; r <= ROUNDS; r++)); do
  echo "--- Round ${r}/${ROUNDS} ---"

  for route in "${ROUTES[@]}"; do
    read -r method path _expected label <<< "${route}"
    request "${method}" "${path}" "${label}"
  done

  # Extra burst of normal requests to generate volume
  for i in {1..5}; do
    request "GET" "/" "burst-${i}"
  done

  if ((r < ROUNDS)); then
    echo "  (pause 2s)"
    sleep 2
  fi
done

echo ""
echo "=== Done — ${ROUNDS} rounds completed ==="
echo ""
echo "Next steps:"
echo "  1. Open Grafana: ${BASE_URL}/grafana (admin/admin)"
echo "  2. Explore > Loki > {namespace=\"podinfo\"} — see the logs"
echo "  3. Dashboards > 'Loki Logs' — browse logs by pod"
echo "  4. Dashboards > 'Node Exporter Full' — node metrics"
echo "  5. Dashboards > 'Kubernetes / Compute Resources / Pod' — pod CPU/RAM"

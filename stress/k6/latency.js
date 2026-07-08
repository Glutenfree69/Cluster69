// Saturation par la LATENCE : on tape /delay/1 (podinfo dort 1s avant de répondre).
// Ça immobilise les connexions -> super pour montrer sur les dashboards
// Grafana la latence p95/p99 qui explose et les requêtes en vol qui s'accumulent.
// N'augmente presque pas le CPU : ici on démontre la saturation, pas l'autoscaling CPU.
//
//   VUS      : nombre de connexions bloquées en parallèle (défaut 50)
//   DELAY    : secondes de sommeil côté podinfo (défaut 1)
//   BASE_URL : cible
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL;
const VUS = parseInt(__ENV.VUS || '50', 10);
const DELAY = __ENV.DELAY || '1';

export const options = {
  scenarios: {
    latency: {
      executor: 'constant-vus',
      vus: VUS,
      duration: '1m',
    },
  },
};

export default function () {
  const res = http.get(`${BASE}/delay/${DELAY}`, { timeout: '30s' });
  check(res, { 'status 200': (r) => r.status === 200 });
}

// Montée en charge progressive pour DÉCLENCHER LE HPA de podinfo.
// On tape /api/info (JSON) : chaque requête consomme un peu de CPU côté pod,
// la conso moyenne dépasse 75% de la request (25m) => le HPA scale 2 -> 6 pods.
//
// Réglable :
//   VUS      : palier max d'utilisateurs virtuels (défaut 40)
//   BASE_URL : cible (injectée par le script shell)
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE = __ENV.BASE_URL;
const MAX_VUS = parseInt(__ENV.VUS || '40', 10);

export const options = {
  scenarios: {
    ramp: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { duration: '30s', target: Math.round(MAX_VUS / 2) }, // chauffe
        { duration: '1m',  target: MAX_VUS },                  // pousse -> HPA doit réagir
        { duration: '1m30s', target: MAX_VUS },                // maintient -> nouveaux pods
        { duration: '30s', target: 0 },                        // relâche -> scale down (après cooldown)
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],       // < 5% d'erreurs
    http_req_duration: ['p(95)<800'],     // p95 sous 800ms
  },
};

export default function () {
  const res = http.get(`${BASE}/api/info`);
  check(res, { 'status 200': (r) => r.status === 200 });
  sleep(0.1);
}

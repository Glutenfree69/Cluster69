// Pic de charge brutal : bien pour montrer la RÉACTIVITÉ (latence qui grimpe
// d'un coup, HPA qui rattrape). Plus spectaculaire que le ramp pour une démo.
//
//   VUS      : hauteur du pic (défaut 100)
//   BASE_URL : cible
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL;
const PEAK = parseInt(__ENV.VUS || '100', 10);

export const options = {
  scenarios: {
    spike: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { duration: '10s', target: 5 },     // calme
        { duration: '5s',  target: PEAK },  // PIC quasi instantané
        { duration: '45s', target: PEAK },  // on tient le pic
        { duration: '10s', target: 0 },     // retour au calme
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.10'],
  },
};

export default function () {
  const res = http.get(`${BASE}/api/info`);
  check(res, { 'status 200': (r) => r.status === 200 });
}

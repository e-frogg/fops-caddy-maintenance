import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '1m', target: 500 },
  ],
};

export default function () {
  let res1 = http.get('http://caddy-with-maintenance:80/');
  check(res1, {
    'caddy with maintenance module': (r) => r.status === 200,
  });

  sleep(0.1);
}
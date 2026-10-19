import http from 'k6/http';
import { fail, sleep } from 'k6';
import { Counter } from 'k6/metrics';

// Short, non-acceptance diagnostic. It deliberately has no thresholds and
// never logs credentials, bearer tokens, headers, or response bodies.
export const options = { vus: 1, duration: '30s' };

const iamBaseUrl = (__ENV.K6_IAM_BASE_URL || 'http://localhost:8081').replace(/\/$/, '');
const referenceBaseUrl = (__ENV.K6_REFERENCE_BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const departmentId = __ENV.R2_DEPARTMENT_ID;
const catalogId = __ENV.R2_CATALOG_ID;

const statusCodes = [200, 401, 403, 429, 500, 503];
const totals = {};
const endpoints = {};
for (const code of statusCodes) totals[code] = new Counter(`status_${code}`);
totals.other = new Counter('status_other');
for (const endpoint of ['query_a', 'query_b', 'query_c']) {
  endpoints[endpoint] = {};
  for (const code of statusCodes) endpoints[endpoint][code] = new Counter(`${endpoint}_status_${code}`);
  endpoints[endpoint].other = new Counter(`${endpoint}_status_other`);
}

export function setup() {
  if (!departmentId || !catalogId) fail('R2_DEPARTMENT_ID and R2_CATALOG_ID are required');
  if (__ENV.K6_ACCESS_TOKEN) return { accessToken: __ENV.K6_ACCESS_TOKEN };
  if (!__ENV.K6_IAM_EMAIL || !__ENV.K6_IAM_PASSWORD) {
    fail('Set K6_IAM_EMAIL and K6_IAM_PASSWORD, or provide K6_ACCESS_TOKEN');
  }
  const response = http.post(
    `${iamBaseUrl}/api/v1/auth/login`,
    JSON.stringify({ email: __ENV.K6_IAM_EMAIL, password: __ENV.K6_IAM_PASSWORD }),
    { headers: { 'Content-Type': 'application/json' }, tags: { endpoint: 'iam_login_setup' } },
  );
  if (response.status !== 200) fail(`IAM login failed with HTTP ${response.status}`);
  const body = response.json();
  if (!body.access_token) fail('IAM login response did not contain access_token');
  return { accessToken: body.access_token };
}

function record(endpoint, url, token) {
  const response = http.get(url, {
    headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
    tags: { endpoint },
  });
  const bucket = Object.prototype.hasOwnProperty.call(totals, response.status) ? response.status : 'other';
  totals[bucket].add(1);
  endpoints[endpoint][bucket].add(1);
}

export default function (data) {
  const selector = __ITER % 3;
  if (selector === 0) {
    record('query_a', `${referenceBaseUrl}/municipalities?department_id=${departmentId}&is_active=true&page=1&page_size=20&sort=name:asc`, data.accessToken);
  } else if (selector === 1) {
    record('query_b', `${referenceBaseUrl}/reports/active-catalog?page=1&page_size=20`, data.accessToken);
  } else {
    record('query_c', `${referenceBaseUrl}/catalogs/${catalogId}/details?page=1&page_size=20&is_active=true&sort=display_order:asc`, data.accessToken);
  }
  sleep(0.1);
}

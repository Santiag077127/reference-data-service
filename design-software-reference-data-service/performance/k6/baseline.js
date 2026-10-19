import http from 'k6/http';
import { check, fail } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const queryAErrors = new Rate('query_a_errors');
const queryBErrors = new Rate('query_b_errors');
const queryCErrors = new Rate('query_c_errors');
const queryADuration = new Trend('query_a_duration', true);
const queryBDuration = new Trend('query_b_duration', true);
const queryCDuration = new Trend('query_c_duration', true);

export const options = {
  vus: 50,
  duration: '5m',
  thresholds: {
    http_req_duration: ['p(95)<300'],
    http_req_failed: ['rate<0.01'],
  },
};

const iamBaseUrl = (__ENV.K6_IAM_BASE_URL || 'http://localhost:8081').replace(/\/$/, '');
const referenceBaseUrl = (__ENV.K6_REFERENCE_BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const departmentId = __ENV.R2_DEPARTMENT_ID;
const catalogId = __ENV.R2_CATALOG_ID;

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

function get(url, endpoint, token) {
  const response = http.get(url, {
    headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
    tags: { endpoint },
  });
  const ok = check(response, { [`${endpoint} returns HTTP 200`]: (r) => r.status === 200 });
  if (endpoint === 'query_a') { queryAErrors.add(!ok); queryADuration.add(response.timings.duration); }
  if (endpoint === 'query_b') { queryBErrors.add(!ok); queryBDuration.add(response.timings.duration); }
  if (endpoint === 'query_c') { queryCErrors.add(!ok); queryCDuration.add(response.timings.duration); }
}

export default function (data) {
  const selector = __ITER % 3;
  if (selector === 0) {
    get(`${referenceBaseUrl}/municipalities?department_id=${departmentId}&is_active=true&page=1&page_size=20&sort=name:asc`, 'query_a', data.accessToken);
  } else if (selector === 1) {
    get(`${referenceBaseUrl}/reports/active-catalog?page=1&page_size=20`, 'query_b', data.accessToken);
  } else {
    get(`${referenceBaseUrl}/catalogs/${catalogId}/details?page=1&page_size=20&is_active=true&sort=display_order:asc`, 'query_c', data.accessToken);
  }
}

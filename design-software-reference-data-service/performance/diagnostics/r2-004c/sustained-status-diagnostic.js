import http from 'k6/http';
import exec from 'k6/execution';
import { fail } from 'k6';
import { Counter } from 'k6/metrics';

// This is a diagnostic run, separate from the acceptance baseline. It has no
// thresholds and never prints or writes credentials, bearer tokens, request
// headers, or response bodies.
export const options = { vus: 50, duration: '5m' };

const iamBaseUrl = (__ENV.K6_IAM_BASE_URL || 'http://localhost:8081').replace(/\/$/, '');
const referenceBaseUrl = (__ENV.K6_REFERENCE_BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const departmentId = __ENV.R2_DEPARTMENT_ID;
const catalogId = __ENV.R2_CATALOG_ID;
const endpoints = ['query_a', 'query_b', 'query_c'];
const statusCodes = [200, 401, 403, 429, 500, 503];
const windows = ['000_030', '030_060', '060_090', '090_120', '120_150', '150_180', '180_210', '210_240', '240_270', '270_300'];

const totalByStatus = {};
const byEndpointWindowStatus = {};
for (const code of statusCodes) totalByStatus[code] = new Counter(`status_${code}`);
totalByStatus.other = new Counter('status_other');
for (const endpoint of endpoints) {
  byEndpointWindowStatus[endpoint] = {};
  for (const window of windows) {
    byEndpointWindowStatus[endpoint][window] = {};
    for (const code of statusCodes) {
      byEndpointWindowStatus[endpoint][window][code] = new Counter(`${endpoint}_w${window}_status_${code}`);
    }
    byEndpointWindowStatus[endpoint][window].other = new Counter(`${endpoint}_w${window}_status_other`);
  }
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

function statusBucket(status) {
  return Object.prototype.hasOwnProperty.call(totalByStatus, status) ? status : 'other';
}

function timeWindow() {
  // currentTestRunDuration is instance-wide, unlike a VU-local start time.
  const index = Math.min(Math.floor(exec.instance.currentTestRunDuration / 30000), windows.length - 1);
  return windows[index];
}

function record(endpoint, url, token) {
  const response = http.get(url, {
    headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
    tags: { endpoint },
  });
  const bucket = statusBucket(response.status);
  totalByStatus[bucket].add(1);
  byEndpointWindowStatus[endpoint][timeWindow()][bucket].add(1);
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
}

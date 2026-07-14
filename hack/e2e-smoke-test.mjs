#!/usr/bin/env node

/**
 * E2E smoke test for the Distr Hub API.
 *
 * Exercises the full user journey: register → login → tutorial flow → verify side effects.
 *
 * Usage:
 *   DISTR_HOST=http://localhost:8080 node hack/e2e-smoke-test.mjs
 *
 * Requires Node.js 18+ (native fetch).
 */

import {randomBytes} from 'node:crypto';

const BASE_URL = (process.env.DISTR_HOST ?? 'http://localhost:8080').replace(/\/$/, '');

const RUN_ID = randomBytes(8).toString('hex');

const TEST_EMAIL = `e2e-${RUN_ID}@smoke.test`;
const TEST_PASSWORD = `Smoke-${randomBytes(24).toString('base64url')}!aA1`;
const TEST_NAME = 'E2E Smoke Test';

async function request(method, path, {body, token} = {}) {
  const headers = {'Content-Type': 'application/json'};
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const res = await fetch(`${BASE_URL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${method} ${path} → ${res.status}: ${text.trim()}`);
  }
  if (res.status === 204) {
    return null;
  }
  return res.json();
}

async function cleanupDemoOrganization(token) {
  const deleteResponse = await fetch(`${BASE_URL}/api/v1/organization`, {
    method: 'DELETE',
    headers: {Authorization: `Bearer ${token}`},
  });
  if (![204, 404].includes(deleteResponse.status)) {
    const text = await deleteResponse.text();
    throw new Error(`DELETE /api/v1/organization cleanup returned ${deleteResponse.status}: ${text.trim()}`);
  }

  const verifyResponse = await fetch(`${BASE_URL}/api/v1/organization`, {
    headers: {Authorization: `Bearer ${token}`},
  });
  if (![401, 403, 404].includes(verifyResponse.status)) {
    const text = await verifyResponse.text();
    throw new Error(`demo organization still accessible after cleanup: ${verifyResponse.status}: ${text.trim()}`);
  }
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(`Assertion failed: ${message}`);
  }
}

let stepNum = 0;
function step(name) {
  stepNum++;
  console.log(`[${stepNum}] ${name}`);
}

step('Register user');
await request('POST', '/api/v1/auth/register', {
  body: {name: TEST_NAME, organizationName: `E2E Smoke ${RUN_ID}`, email: TEST_EMAIL, password: TEST_PASSWORD},
});

step('Login');
const loginResponse = await request('POST', '/api/v1/auth/login', {
  body: {email: TEST_EMAIL, password: TEST_PASSWORD},
});
const token = loginResponse.token;
assert(token, 'login response must include a token');

step('Verify organization exists');
const org = await request('GET', '/api/v1/organization', {token});
assert(org && org.name, 'organization must have a name');

let deploymentTargetId;
let applicationId;

try {
  step('Trigger tutorial (agents/welcome/start)');
  const tutorialResult = await request('PUT', '/api/v1/tutorial-progress/agents', {
    token,
    body: {stepId: 'welcome', taskId: 'start'},
  });
  const tutorialEvent = tutorialResult?.events?.find((e) => e.stepId === 'welcome' && e.taskId === 'start');
  assert(tutorialEvent?.value?.deploymentTargetId, 'tutorial response must include an event with deploymentTargetId');
  deploymentTargetId = tutorialEvent.value.deploymentTargetId;

  step('Verify hello-distr application was created');
  const applications = await request('GET', '/api/v1/applications', {token});
  const helloApp = applications.find((a) => a.name === 'hello-distr');
  assert(helloApp, 'hello-distr application must exist');
  applicationId = helloApp.id;

  step('Verify hello-distr-tutorial deployment target was created with a deployment');
  const targets = await request('GET', '/api/v1/deployment-targets', {token});
  const helloTarget = targets.find((t) => t.id === deploymentTargetId);
  assert(helloTarget, `deployment target ${deploymentTargetId} must exist`);
  assert(helloTarget.name === 'hello-distr-tutorial', 'deployment target must be named hello-distr-tutorial');
  assert(helloTarget.deployments?.length > 0, 'hello-distr-tutorial must have at least one deployment');

  console.log(`\nAll ${stepNum} smoke test steps passed.`);
} finally {
  if (deploymentTargetId) {
    await request('DELETE', `/api/v1/deployment-targets/${deploymentTargetId}`, {token});
  }
  if (applicationId) {
    await request('DELETE', `/api/v1/applications/${applicationId}`, {token});
  }
  if (token) {
    await cleanupDemoOrganization(token);
  }
}

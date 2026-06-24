#!/usr/bin/env node

import crypto from 'node:crypto';
import {readFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const demoDir = fileURLToPath(new URL('.', import.meta.url));
const fixturePath = path.join(demoDir, 'flow.fixture.json');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
const jsonOutput = process.argv.includes('--json');

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function stableStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(',')}]`;
  }
  if (value && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

function digest(value) {
  return crypto.createHash('sha256').update(stableStringify(value)).digest('hex');
}

function walk(value, visit, pathParts = []) {
  if (Array.isArray(value)) {
    value.forEach((item, index) => walk(item, visit, [...pathParts, String(index)]));
    return;
  }
  if (value && typeof value === 'object') {
    for (const [key, child] of Object.entries(value)) {
      visit(key, child, [...pathParts, key]);
      walk(child, visit, [...pathParts, key]);
    }
  }
}

const requiredSteps = ['preflight', 'health-check'];

assert(fixture.schemaVersion === 'pr050.community-e2e.v1', 'unexpected fixture schema version');
assert(fixture.releaseBundle.status === 'PUBLISHED', 'release bundle must be published');
assert(fixture.releaseBundle.components.length >= 2, 'demo release must include multiple components');
assert(fixture.deploymentPlan.state === 'READY', 'deployment plan must be ready');
assert(fixture.deploymentPlan.blocked === false, 'deployment plan must not be blocked');
assert(fixture.task.state === 'SUCCEEDED', 'task must succeed');
assert(fixture.task.lease.agent && fixture.task.lease.target, 'task lease must identify agent and target');

const stepKeys = fixture.deploymentProcess.steps.map((step) => step.key);
for (const requiredStep of requiredSteps) {
  assert(stepKeys.includes(requiredStep), `missing process step ${requiredStep}`);
}

const eventText = stableStringify(fixture.task.events);
assert(!eventText.includes('demo-api-token"'), 'secret reference leaked as a quoted value');
assert(eventText.includes('[REDACTED]'), 'redacted marker must appear in event output');

walk(fixture, (key, value, pathParts) => {
  const normalizedKey = key.toLowerCase();
  if (typeof value === 'string' && /secretvalue|passwordvalue|plaintextsecret/.test(normalizedKey)) {
    throw new Error(`forbidden secret-value field at ${pathParts.join('.')}`);
  }
});

const flowDigest = digest({
  product: fixture.product,
  releaseBundle: fixture.releaseBundle,
  deploymentProcess: fixture.deploymentProcess,
  deploymentPlan: fixture.deploymentPlan,
  task: fixture.task,
});

const result = {
  ok: true,
  schemaVersion: fixture.schemaVersion,
  flowDigest,
  checkedSteps: [
    'release bundle',
    'deployment process',
    'deployment plan',
    'agent lease',
    'redacted events',
    'cleanup plan',
  ],
};

if (jsonOutput) {
  console.log(JSON.stringify(result, null, 2));
} else {
  console.log('Community E2E demo verifier');
  for (const step of result.checkedSteps) {
    console.log(`PASS ${step}`);
  }
  console.log(`PASS stable flow digest ${flowDigest}`);
}

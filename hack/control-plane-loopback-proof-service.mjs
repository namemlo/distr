#!/usr/bin/env node

import {createHash} from 'node:crypto';
import {readFile} from 'node:fs/promises';
import {createServer} from 'node:http';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

import {validateFixture as validateBenchmarkFixture} from './control-plane-read-model-benchmark.mjs';

const maximumRequestBytes = 4 * 1024 * 1024;
const proofHeaders = {
  sourceCommit: 'x-distr-proof-source-commit',
  buildVersion: 'x-distr-proof-build-version',
  artifactDigest: 'x-distr-proof-artifact-digest',
};

function fail(message) {
  throw new Error(message);
}

function sha256(value) {
  return `sha256:${createHash('sha256').update(value).digest('hex')}`;
}

function deterministicID(namespace, index) {
  const hex = createHash('sha256').update(`${namespace}:${index}`).digest('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-5${hex.slice(13, 16)}-a${hex.slice(17, 20)}-${hex.slice(20, 32)}`;
}

function canonicalPlanningChecksum(components) {
  const input = components
    .map(({id, key, releaseDigest}) => ({id, key, releaseDigest}))
    .toSorted((left, right) => left.id.localeCompare(right.id));
  return sha256(JSON.stringify(input));
}

function waveOrderChecksum(members) {
  return sha256(JSON.stringify(members.map(({id, order, planId, targetId}) => ({id, order, planId, targetId}))));
}

async function readJSONBody(request) {
  const chunks = [];
  let bytes = 0;
  for await (const chunk of request) {
    bytes += chunk.byteLength;
    if (bytes > maximumRequestBytes) fail('request body exceeds the loopback proof bound');
    chunks.push(chunk);
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString('utf8'));
  } catch {
    fail('request body must be valid JSON');
  }
}

function writeJSON(response, status, payload, metadata) {
  response.statusCode = status;
  response.setHeader('content-type', 'application/json');
  for (const [field, header] of Object.entries(proofHeaders)) response.setHeader(header, metadata[field]);
  response.end(JSON.stringify(payload));
}

function boundedPage(rows, url) {
  const requested = Number(url.searchParams.get('limit') ?? '100');
  const limit = Number.isSafeInteger(requested) && requested >= 1 && requested <= 100 ? requested : 100;
  return {items: rows.slice(0, limit), total: rows.length};
}

function buildReadModels(fixture) {
  const resources = fixture.benchmark.resources;
  const primaryTargets = fixture.targets.filter((target) => target.organizationId === fixture.primaryOrganization.id);
  const primaryComponents = fixture.components.filter(
    (component) => component.organizationId === fixture.primaryOrganization.id
  );
  const primaryFleet = fixture.operatorReadModels.fleetRows.filter(
    (row) => row.organizationId === fixture.primaryOrganization.id
  );
  const releases = Array.from({length: 100}, (_, index) => ({
    id: resources.releaseIds[index] ?? deterministicID('loopback-release', index),
    kind: index % 2 === 0 ? 'component' : 'product',
    status: 'PUBLISHED',
    version: `1.${index}.0`,
    checksum: primaryComponents[index % primaryComponents.length].releaseDigest,
  }));
  const fleet = primaryFleet.slice(0, 100).map((row, index) => ({
    id: fixture.placements[index].id,
    deploymentTargetId: row.targetId,
    environmentId: row.environmentId,
    component: row.componentKey,
    observedState: row.observedState,
    drift: row.drift,
  }));
  const plans = Array.from({length: 100}, (_, index) => ({
    id: resources.planIds[index] ?? fixture.campaign.waves[0].members[index].planId,
    status: 'READY',
    stepCount: fixture.loadProof.wave.stepCount,
    canonicalChecksum: sha256(`plan:${index}`),
  }));
  const executions = Array.from({length: 100}, (_, index) => ({
    id: index === 0 ? resources.executionId : fixture.steps[index].id,
    deploymentPlanId: plans[index].id,
    deploymentTargetId: primaryTargets[index % primaryTargets.length].id,
    status: 'SUCCEEDED',
  }));
  const campaign = {
    id: resources.campaignId,
    status: 'RUNNING',
    waveCount: 1,
    memberCount: fixture.campaign.waves[0].members.length,
  };
  return {releases, fleet, plans, executions, campaign};
}

function readModelResponse(fixture, models, name, url) {
  switch (name) {
    case 'registry-list':
      return boundedPage(models.releases, url);
    case 'registry-detail':
      return {detail: {release: models.releases[0]}};
    case 'matrix-list':
      return boundedPage(models.fleet, url);
    case 'matrix-detail':
      return {items: models.fleet.filter((row) => row.deploymentTargetId === fixture.targets[0].id), total: 1};
    case 'comparison-list':
      return boundedPage(models.plans, url);
    case 'comparison-detail':
      return {comparison: {left: models.plans[0], right: models.plans[1], changes: []}};
    case 'history-list':
      return boundedPage(models.executions, url);
    case 'history-detail':
      return {detail: {execution: models.executions[0], attempts: [], observations: []}};
    case 'campaign-list':
      return {items: [models.campaign], total: 1};
    case 'campaign-detail':
      return {detail: {campaign: models.campaign, waves: fixture.campaign.waves, members: []}};
    default:
      return undefined;
  }
}

function matchesRequestDescriptor(descriptor, url) {
  const expected = new URL(descriptor.path, 'http://loopback.invalid');
  if (expected.pathname !== url.pathname) return false;
  for (const [key, value] of expected.searchParams) {
    if (url.searchParams.get(key) !== value) return false;
  }
  for (const [key, value] of url.searchParams) {
    if (key !== 'limit' && expected.searchParams.get(key) !== value) return false;
  }
  return true;
}

export async function startLoopbackProofService({fixture, token, metadata, port = 0}) {
  validateBenchmarkFixture(fixture, {profile: 'acceptance'});
  if (typeof token !== 'string' || token.length < 16) fail('loopback proof token must contain at least 16 characters');
  if (!/^[a-f0-9]{40}$/.test(metadata?.sourceCommit ?? '')) fail('sourceCommit must be a full Git commit');
  if (!/^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$/.test(metadata?.buildVersion ?? '')) {
    fail('buildVersion must be a non-secret build identifier');
  }
  if (!/^sha256:[0-9a-f]{64}$/.test(metadata?.artifactDigest ?? '')) {
    fail('artifactDigest must be a lowercase SHA-256 digest');
  }

  const models = buildReadModels(fixture);
  const agentIDs = new Set(fixture.agents.map((agent) => agent.id));
  const activeAgentIDs = new Set();
  const logPage = Buffer.alloc(fixture.loadProof.logs.maximumPageBytes, 97);
  const logPageCount = fixture.loadProof.logs.totalBytes / logPage.byteLength;
  const counters = {requests: 0, unauthorized: 0, planning: 0, waves: 0, events: 0, logPages: 0};

  const server = createServer(async (request, response) => {
    counters.requests++;
    const url = new URL(request.url ?? '/', 'http://127.0.0.1');
    if (url.pathname === '/healthz') {
      writeJSON(response, 200, {status: 'ready'}, metadata);
      return;
    }
    if (request.headers.authorization !== `Bearer ${token}`) {
      counters.unauthorized++;
      writeJSON(response, 401, {error: 'unauthorized'}, metadata);
      return;
    }

    try {
      const descriptor = fixture.benchmark.remoteRequests.find((candidate) => matchesRequestDescriptor(candidate, url));
      if (request.method === 'GET' && descriptor) {
        writeJSON(response, 200, readModelResponse(fixture, models, descriptor.name, url), metadata);
        return;
      }
      if (request.method === 'POST' && url.pathname === fixture.loadProof.remote.planningPath) {
        const body = await readJSONBody(request);
        if (!Array.isArray(body.components) || body.components.length !== fixture.loadProof.planning.componentCount) {
          writeJSON(response, 400, {error: 'invalid planning workload'}, metadata);
          return;
        }
        counters.planning++;
        writeJSON(response, 200, {checksum: canonicalPlanningChecksum(body.components)}, metadata);
        return;
      }
      if (request.method === 'POST' && url.pathname === fixture.loadProof.remote.wavePath) {
        const body = await readJSONBody(request);
        const members = body.members;
        const uniqueIDs = new Set(Array.isArray(members) ? members.map((member) => member.id) : []);
        const stableOrder =
          Array.isArray(members) && members.every((member, index) => member.order === index + 1);
        if (!Array.isArray(members) || members.length !== fixture.loadProof.wave.stepCount) {
          writeJSON(response, 400, {error: 'invalid wave workload'}, metadata);
          return;
        }
        counters.waves++;
        writeJSON(
          response,
          200,
          {
            stepCount: members.length,
            stableOrder,
            duplicateAdmissions: members.length - uniqueIDs.size,
            orderChecksum: waveOrderChecksum(members),
          },
          metadata
        );
        return;
      }
      if (request.method === 'POST' && url.pathname === fixture.loadProof.remote.eventPath) {
        const body = await readJSONBody(request);
        if (
          typeof body.eventId !== 'string' ||
          !agentIDs.has(body.agentId) ||
          !Number.isSafeInteger(body.sequence) ||
          body.sequence < 1
        ) {
          writeJSON(response, 400, {error: 'invalid authenticated event'}, metadata);
          return;
        }
        counters.events++;
        activeAgentIDs.add(body.agentId);
        writeJSON(response, 200, {accepted: true, eventId: body.eventId, agentId: body.agentId}, metadata);
        return;
      }
      if (request.method === 'GET' && url.pathname === fixture.loadProof.remote.logPath) {
        const page = Number(url.searchParams.get('page'));
        if (!Number.isSafeInteger(page) || page < 0 || page >= logPageCount) {
          writeJSON(response, 404, {error: 'log page not found'}, metadata);
          return;
        }
        counters.logPages++;
        response.statusCode = 200;
        response.setHeader('content-type', 'application/octet-stream');
        response.setHeader('content-length', String(logPage.byteLength));
        response.setHeader('x-next-page', page + 1 < logPageCount ? String(page + 1) : '');
        response.setHeader('x-distr-proof-peak-buffer-bytes', String(logPage.byteLength));
        for (const [field, header] of Object.entries(proofHeaders)) response.setHeader(header, metadata[field]);
        response.end(logPage);
        return;
      }
      writeJSON(response, 404, {error: 'not found'}, metadata);
    } catch (error) {
      writeJSON(response, 400, {error: error.message}, metadata);
    }
  });

  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(port, '127.0.0.1', resolve);
  });
  const address = server.address();
  return {
    baseURL: new URL(`http://127.0.0.1:${address.port}`),
    snapshot: () => ({...counters, activeAgentIds: [...activeAgentIDs].sort()}),
    close: () => new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve()))),
  };
}

function parseArgs(argv) {
  const values = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    const option = argv[index];
    const value = argv[index + 1];
    if (!option?.startsWith('--') || value === undefined || values.has(option)) fail(`invalid argument near ${option}`);
    values.set(option, value);
  }
  const fixture = values.get('--fixture');
  const sourceCommit = values.get('--source-commit');
  const buildVersion = values.get('--build-version');
  const artifactDigest = values.get('--artifact-digest');
  const authEnv = values.get('--auth-env') ?? 'CONTROL_PLANE_LOOPBACK_PROOF_TOKEN';
  const port = Number(values.get('--port') ?? '0');
  if (!fixture) fail('--fixture is required');
  if (!Number.isSafeInteger(port) || port < 0 || port > 65535) fail('--port must be between 0 and 65535');
  if (!process.env[authEnv]) fail(`authorization is required from environment variable ${authEnv}`);
  return {fixture: path.resolve(fixture), sourceCommit, buildVersion, artifactDigest, authEnv, port};
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const fixture = JSON.parse(await readFile(options.fixture, 'utf8'));
  const service = await startLoopbackProofService({
    fixture,
    token: process.env[options.authEnv],
    metadata: {
      sourceCommit: options.sourceCommit,
      buildVersion: options.buildVersion,
      artifactDigest: options.artifactDigest,
    },
    port: options.port,
  });
  process.stdout.write(`${service.baseURL.href}\n`);
  const close = async () => {
    await service.close();
    process.exitCode = 0;
  };
  process.once('SIGINT', close);
  process.once('SIGTERM', close);
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}

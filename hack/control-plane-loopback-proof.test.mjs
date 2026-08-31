import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtemp, readFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {test} from 'node:test';

import {startLoopbackProofService} from './control-plane-loopback-proof-service.mjs';

const repoRoot = path.resolve(new URL('..', import.meta.url).pathname.slice(process.platform === 'win32' ? 1 : 0));
const fixtureScript = path.join(repoRoot, 'hack', 'control-plane-scale-fixture.mjs');

async function referenceFixture() {
  const directory = await mkdtemp(path.join(tmpdir(), 'control-plane-loopback-proof-'));
  const output = path.join(directory, 'fixture.json');
  const result = spawnSync(
    process.execPath,
    [
      fixtureScript,
      '--targets',
      '1000',
      '--placements',
      '649',
      '--agents',
      '100',
      '--components',
      '100',
      '--steps',
      '500',
      '--out',
      output,
    ],
    {cwd: repoRoot, encoding: 'utf8'}
  );
  assert.equal(result.status, 0, result.stderr);
  return JSON.parse(await readFile(output, 'utf8'));
}

test('loopback proof service authenticates and deterministically implements every scale/load endpoint', async () => {
  const fixture = await referenceFixture();
  const token = 'loopback-proof-test-token';
  const metadata = {
    sourceCommit: 'a'.repeat(40),
    buildVersion: 'loopback-test',
    artifactDigest: `sha256:${'b'.repeat(64)}`,
  };
  const service = await startLoopbackProofService({fixture, token, metadata});
  const request = (requestPath, init = {}) =>
    fetch(new URL(requestPath, service.baseURL), {
      ...init,
      headers: {authorization: `Bearer ${token}`, ...init.headers},
      redirect: 'error',
    });
  try {
    const unauthorized = await fetch(new URL(fixture.loadProof.remote.eventPath, service.baseURL), {
      method: 'POST',
      headers: {'content-type': 'application/json'},
      body: JSON.stringify({eventId: 'event-unauthorized', agentId: fixture.agents[0].id, sequence: 1}),
    });
    assert.equal(unauthorized.status, 401);
    assert.doesNotMatch(await unauthorized.text(), new RegExp(token));

    const planningChecksums = [];
    for (let run = 0; run < fixture.loadProof.planning.runs; run++) {
      const response = await request(fixture.loadProof.remote.planningPath, {
        method: 'POST',
        headers: {'content-type': 'application/json'},
        body: JSON.stringify({run, components: fixture.components.slice(0, 100)}),
      });
      assert.equal(response.status, 200);
      planningChecksums.push((await response.json()).checksum);
    }
    assert.equal(new Set(planningChecksums).size, 1);
    assert.match(planningChecksums[0], /^sha256:[a-f0-9]{64}$/);

    const waveChecksums = [];
    for (let run = 0; run < fixture.loadProof.wave.runs; run++) {
      const response = await request(fixture.loadProof.remote.wavePath, {
        method: 'POST',
        headers: {'content-type': 'application/json'},
        body: JSON.stringify({run, members: fixture.campaign.waves[0].members}),
      });
      const payload = await response.json();
      assert.deepEqual(
        {
          stepCount: payload.stepCount,
          stableOrder: payload.stableOrder,
          duplicateAdmissions: payload.duplicateAdmissions,
        },
        {stepCount: 500, stableOrder: true, duplicateAdmissions: 0}
      );
      waveChecksums.push(payload.orderChecksum);
    }
    assert.equal(new Set(waveChecksums).size, 1);
    assert.match(waveChecksums[0], /^sha256:[a-f0-9]{64}$/);

    const eventResponses = await Promise.all(
      fixture.agents.map((agent, index) =>
        request(fixture.loadProof.remote.eventPath, {
          method: 'POST',
          headers: {'content-type': 'application/json'},
          body: JSON.stringify({eventId: `event-${index + 1}`, agentId: agent.id, sequence: index + 1}),
        }).then((response) => response.json())
      )
    );
    assert.equal(eventResponses.length, 100);
    assert.equal(new Set(eventResponses.map((response) => response.agentId)).size, 100);
    assert.ok(eventResponses.every((response) => response.accepted === true));

    const logResponse = await request(`${fixture.loadProof.remote.logPath}?page=0`);
    assert.equal(logResponse.status, 200);
    assert.equal(Number(logResponse.headers.get('x-distr-proof-peak-buffer-bytes')), 1024 * 1024);
    assert.equal((await logResponse.arrayBuffer()).byteLength, 1024 * 1024);

    for (const descriptor of fixture.benchmark.remoteRequests) {
      const response = await request(descriptor.path);
      assert.equal(response.status, 200, descriptor.name);
      assert.equal(response.headers.get('x-distr-proof-source-commit'), metadata.sourceCommit);
      assert.doesNotMatch(await response.text(), new RegExp(fixture.isolationSentinel.organization.id));
    }

    const snapshot = service.snapshot();
    assert.equal(snapshot.unauthorized, 1);
    assert.equal(snapshot.planning, 5);
    assert.equal(snapshot.waves, 2);
    assert.equal(snapshot.events, 100);
    assert.equal(snapshot.logPages, 1);
    assert.equal(snapshot.activeAgentIds.length, 100);
  } finally {
    await service.close();
  }
});

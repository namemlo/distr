#!/usr/bin/env node

import {createHash} from 'node:crypto';
import {mkdir, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const schemaVersion = 'distr.control-plane-scale-fixture/v1';
const deterministicInstant = '2026-01-01T00:00:00.000Z';
const floors = {targets: 1000, placements: 649, agents: 100, components: 100, steps: 500};

function fail(message) {
  throw new Error(message);
}

function parseInteger(value, option, minimum) {
  if (!/^\d+$/.test(value ?? '')) {
    fail(`${option} must be an integer at least ${minimum}`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < minimum) {
    fail(`${option} must be an integer at least ${minimum}`);
  }
  return parsed;
}

export function parseFixtureArgs(argv) {
  const values = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    const option = argv[index];
    const value = argv[index + 1];
    if (!option?.startsWith('--') || value === undefined) {
      fail(`invalid argument near ${option ?? '<end>'}`);
    }
    if (values.has(option)) {
      fail(`${option} may be supplied only once`);
    }
    values.set(option, value);
  }
  const allowed = new Set(['--targets', '--placements', '--agents', '--components', '--steps', '--out']);
  for (const option of values.keys()) {
    if (!allowed.has(option)) {
      fail(`unknown option ${option}`);
    }
  }
  const output = values.get('--out');
  if (!output?.trim()) {
    fail('--out is required');
  }
  const parameters = {};
  for (const [name, minimum] of Object.entries(floors)) {
    parameters[name] = parseInteger(values.get(`--${name}`), `--${name}`, minimum);
  }
  return {parameters, output: path.resolve(output)};
}

function stableUUID(namespace, index) {
  const hex = createHash('sha256').update(`${schemaVersion}:${namespace}:${index}`).digest('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-5${hex.slice(13, 16)}-a${hex.slice(17, 20)}-${hex.slice(20, 32)}`;
}

function numbered(prefix, index, width = 4) {
  return `${prefix}-${String(index + 1).padStart(width, '0')}`;
}

function digest(namespace, index) {
  return `sha256:${createHash('sha256').update(`${namespace}:${index}`).digest('hex')}`;
}

export function buildScaleFixture(parameters) {
  for (const [name, minimum] of Object.entries(floors)) {
    if (!Number.isSafeInteger(parameters[name]) || parameters[name] < minimum) {
      fail(`--${name} must be an integer at least ${minimum}`);
    }
  }

  const primaryOrganization = {
    id: stableUUID('organization', 0),
    name: 'tenant-primary',
  };
  const sentinelOrganization = {
    id: stableUUID('organization', 1),
    name: 'tenant-isolation-sentinel',
  };
  const environments = Array.from({length: 25}, (_, index) => ({
    id: stableUUID('environment', index),
    organizationId: primaryOrganization.id,
    name: numbered('environment', index, 2),
    lifecycle: ['development', 'test', 'staging', 'production'][index % 4],
  }));
  const components = Array.from({length: parameters.components}, (_, index) => ({
    id: stableUUID('component', index),
    organizationId: primaryOrganization.id,
    key: numbered('component', index, 3),
    releaseDigest: digest('component-release', index),
  }));
  const targets = Array.from({length: parameters.targets}, (_, index) => ({
    id: stableUUID('target', index),
    organizationId: primaryOrganization.id,
    environmentId: environments[index % environments.length].id,
    agentId: stableUUID('agent', index % parameters.agents),
    name: numbered('target', index),
    status: ['online', 'online', 'online', 'offline'][index % 4],
  }));
  const agents = Array.from({length: parameters.agents}, (_, index) => ({
    id: stableUUID('agent', index),
    organizationId: primaryOrganization.id,
    name: numbered('agent', index, 3),
    targetIds: targets.filter((_, targetIndex) => targetIndex % parameters.agents === index).map((row) => row.id),
  }));
  const placements = Array.from({length: parameters.placements}, (_, index) => ({
    id: stableUUID('placement', index),
    organizationId: primaryOrganization.id,
    targetId: targets[index % targets.length].id,
    componentId: components[index % components.length].id,
    unitKey: numbered('unit', index),
    ordinal: index + 1,
  }));
  const steps = Array.from({length: parameters.steps}, (_, index) => ({
    id: stableUUID('step', index),
    organizationId: primaryOrganization.id,
    key: numbered('step', index),
    memberId: stableUUID('campaign-member', index),
    targetId: targets[index % targets.length].id,
    componentId: components[index % components.length].id,
    order: index + 1,
  }));

  const tieInstant = '2025-12-31T23:59:00.000Z';
  const fleetRows = targets.map((target, index) => {
    const placement = placements[index % placements.length];
    const component = components[index % components.length];
    return {
      organizationId: primaryOrganization.id,
      environmentId: target.environmentId,
      targetId: target.id,
      targetName: target.name,
      unitKey: placement.unitKey,
      componentKey: component.key,
      activeRelease: component.releaseDigest,
      pendingRelease: index % 7 === 0 ? digest('pending-release', index) : null,
      observedState: index % 11 === 0 ? 'unknown' : 'current',
      drift: index % 13 === 0 ? 'detected' : 'none',
      enrollment: index % 17 === 0 ? 'pending' : 'enrolled',
      lastExecutionAt: index < 4 ? tieInstant : new Date(Date.parse(tieInstant) - (index - 3) * 1000).toISOString(),
    };
  });
  const sentinelTarget = {
    id: stableUUID('sentinel-target', 0),
    organizationId: sentinelOrganization.id,
    environmentId: stableUUID('sentinel-environment', 0),
    name: 'sentinel-target',
  };
  const sentinelFleetRow = {
    organizationId: sentinelOrganization.id,
    environmentId: sentinelTarget.environmentId,
    targetId: sentinelTarget.id,
    targetName: sentinelTarget.name,
    unitKey: 'sentinel-unit',
    componentKey: 'sentinel-component',
    activeRelease: digest('sentinel-release', 0),
    pendingRelease: null,
    observedState: 'current',
    drift: 'none',
    enrollment: 'enrolled',
    lastExecutionAt: tieInstant,
  };
  const waveMembers = Array.from({length: 500}, (_, index) => ({
    id: stableUUID('campaign-member', index),
    planId: stableUUID('plan', index),
    targetId: targets[index].id,
    order: index + 1,
  }));
  const environmentFilter = environments[0];

  return {
    schemaVersion,
    seed: 'control-plane-reference-v1',
    generatedAt: deterministicInstant,
    parameters: {...parameters},
    primaryOrganization,
    isolationSentinel: {
      organization: sentinelOrganization,
      target: sentinelTarget,
    },
    environments,
    targets,
    placements,
    agents,
    components,
    steps,
    campaign: {
      id: stableUUID('campaign', 0),
      organizationId: primaryOrganization.id,
      waves: [
        {
          id: stableUUID('campaign-wave', 0),
          order: 1,
          members: waveMembers,
        },
      ],
    },
    operatorReadModels: {
      fleetRows: [...fleetRows, sentinelFleetRow],
    },
    expectations: {
      maximumPageSize: 100,
      cursorTie: {
        sortField: 'lastExecutionAt',
        value: tieInstant,
        orderedTargetIds: fleetRows
          .slice(0, 4)
          .map((row) => row.targetId)
          .sort(),
      },
      filters: {
        environment: {
          id: environmentFilter.id,
          targetIds: targets
            .filter((target) => target.environmentId === environmentFilter.id)
            .map((target) => target.id),
        },
        drift: {
          value: 'detected',
          targetIds: fleetRows.filter((row) => row.drift === 'detected').map((row) => row.targetId),
        },
      },
    },
    benchmark: {
      remoteRequests: [
        {name: 'fleet-list', path: '/api/v1/control-plane/fleet?limit=100'},
        {name: 'campaign-list', path: '/api/v1/control-plane/campaigns?limit=100'},
        {name: 'execution-list', path: '/api/v1/control-plane/executions?limit=100'},
      ],
    },
  };
}

export async function writeScaleFixture(output, fixture) {
  await mkdir(path.dirname(output), {recursive: true});
  await writeFile(output, `${JSON.stringify(fixture, null, 2)}\n`, {encoding: 'utf8', flag: 'w'});
}

async function main() {
  const {parameters, output} = parseFixtureArgs(process.argv.slice(2));
  await writeScaleFixture(output, buildScaleFixture(parameters));
  process.stdout.write(`${output}\n`);
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : '';
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}

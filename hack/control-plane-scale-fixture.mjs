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

  const clientOrganizationCount = 25;
  const servicesPerOrganization = 25;
  const clientOrganizations = Array.from({length: clientOrganizationCount}, (_, index) => ({
    id: stableUUID('organization', index),
    name: numbered('client-organization', index, 2),
  }));
  const primaryOrganization = clientOrganizations[0];
  const sentinelOrganization = {
    id: stableUUID('organization', clientOrganizationCount),
    name: 'tenant-isolation-sentinel',
  };
  const environments = clientOrganizations.map((organization, index) => ({
    id: stableUUID('environment', index),
    organizationId: organization.id,
    name: numbered('environment', index, 2),
    lifecycle: ['development', 'test', 'staging', 'production'][index % 4],
  }));
  const componentDefinitionCount = Math.max(parameters.components, clientOrganizationCount * servicesPerOrganization);
  const components = Array.from({length: componentDefinitionCount}, (_, index) => {
    const organizationIndex = index % clientOrganizationCount;
    const serviceIndex = Math.floor(index / clientOrganizationCount);
    return {
      id: stableUUID('component', index),
      organizationId: clientOrganizations[organizationIndex].id,
      key: numbered('service', serviceIndex, 3),
      releaseDigest: digest('component-release', index),
    };
  });
  const serviceCatalogByOrganization = new Map(
    clientOrganizations.map((organization) => [
      organization.id,
      components.filter((component) => component.organizationId === organization.id),
    ])
  );
  const agentRows = Array.from({length: parameters.agents}, (_, index) => {
    const organization = clientOrganizations[index % clientOrganizationCount];
    return {
      id: stableUUID('agent', index),
      organizationId: organization.id,
      name: numbered('agent', index, 3),
    };
  });
  const agentsByOrganization = new Map(
    clientOrganizations.map((organization) => [
      organization.id,
      agentRows.filter((agent) => agent.organizationId === organization.id),
    ])
  );
  const targets = Array.from({length: parameters.targets}, (_, index) => {
    const organizationIndex = index % clientOrganizationCount;
    const organization = clientOrganizations[organizationIndex];
    const organizationAgents = agentsByOrganization.get(organization.id);
    const localIndex = Math.floor(index / clientOrganizationCount);
    return {
      id: stableUUID('target', index),
      organizationId: organization.id,
      environmentId: environments[organizationIndex].id,
      agentId: organizationAgents[localIndex % organizationAgents.length].id,
      name: numbered('target', index),
      status: ['online', 'online', 'online', 'offline'][index % 4],
    };
  });
  const agents = agentRows.map((agent) => ({
    ...agent,
    targetIds: targets.filter((target) => target.agentId === agent.id).map((target) => target.id),
  }));
  const targetsByOrganization = new Map(
    clientOrganizations.map((organization) => [
      organization.id,
      targets.filter((target) => target.organizationId === organization.id),
    ])
  );
  const placements = Array.from({length: parameters.placements}, (_, index) => {
    const organizationIndex = index % clientOrganizationCount;
    const organization = clientOrganizations[organizationIndex];
    const localIndex = Math.floor(index / clientOrganizationCount);
    const organizationTargets = targetsByOrganization.get(organization.id);
    const organizationComponents = serviceCatalogByOrganization.get(organization.id);
    return {
      id: stableUUID('placement', index),
      organizationId: organization.id,
      targetId: organizationTargets[localIndex % organizationTargets.length].id,
      componentId: organizationComponents[localIndex % organizationComponents.length].id,
      unitKey: numbered('unit', index),
      ordinal: index + 1,
    };
  });
  const placementsByOrganization = new Map(
    clientOrganizations.map((organization) => [
      organization.id,
      placements.filter((placement) => placement.organizationId === organization.id),
    ])
  );
  const componentByID = new Map(components.map((component) => [component.id, component]));
  const primaryTargets = targetsByOrganization.get(primaryOrganization.id);
  const primaryComponents = serviceCatalogByOrganization.get(primaryOrganization.id);
  const steps = Array.from({length: parameters.steps}, (_, index) => ({
    id: stableUUID('step', index),
    organizationId: primaryOrganization.id,
    key: numbered('step', index),
    memberId: stableUUID('campaign-member', index),
    targetId: primaryTargets[index % primaryTargets.length].id,
    componentId: primaryComponents[index % primaryComponents.length].id,
    order: index + 1,
  }));

  const tieInstant = '2025-12-31T23:59:00.000Z';
  const fleetRows = targets.map((target, index) => {
    const organizationPlacements = placementsByOrganization.get(target.organizationId);
    const localIndex = Math.floor(index / clientOrganizationCount);
    const placement = organizationPlacements[localIndex % organizationPlacements.length];
    const component = componentByID.get(placement.componentId);
    return {
      organizationId: target.organizationId,
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
  const sentinelCampaign = {
    id: stableUUID('sentinel-campaign', 0),
    organizationId: sentinelOrganization.id,
    name: 'sentinel-campaign',
    status: 'RUNNING',
  };
  const sentinelExecution = {
    id: stableUUID('sentinel-execution', 0),
    organizationId: sentinelOrganization.id,
    campaignId: sentinelCampaign.id,
    deploymentPlanId: stableUUID('sentinel-plan', 0),
    deploymentTargetId: sentinelTarget.id,
    status: 'RUNNING',
  };
  const waveMembers = Array.from({length: 500}, (_, index) => ({
    id: stableUUID('campaign-member', index),
    planId: stableUUID('plan', index),
    targetId: primaryTargets[index % primaryTargets.length].id,
    order: index + 1,
  }));
  const environmentFilter = environments[0];
  const otherClientTargetIDs = clientOrganizations
    .slice(1)
    .map((organization) => targetsByOrganization.get(organization.id)[0].id);

  return {
    schemaVersion,
    seed: 'control-plane-reference-v1',
    generatedAt: deterministicInstant,
    parameters: {...parameters},
    primaryOrganization,
    clientOrganizations,
    isolationSentinel: {
      organization: sentinelOrganization,
      target: sentinelTarget,
      campaign: sentinelCampaign,
      execution: sentinelExecution,
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
      multiClient: {
        organizationCount: clientOrganizationCount,
        minimumPlacementsPerOrganization: servicesPerOrganization,
        minimumDistinctServicesPerOrganization: servicesPerOrganization,
      },
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
        {
          name: 'fleet-list',
          path: '/api/v1/control-plane/fleet?limit=100',
          forbiddenResourceIds: [...otherClientTargetIDs, sentinelTarget.id],
        },
        {
          name: 'campaign-list',
          path: '/api/v1/control-plane/campaigns?limit=100',
          forbiddenResourceIds: [sentinelCampaign.id],
        },
        {
          name: 'execution-list',
          path: '/api/v1/control-plane/executions?limit=100',
          forbiddenResourceIds: [sentinelExecution.id, sentinelTarget.id],
        },
      ],
    },
    loadProof: {
      planning: {componentCount: 100, runs: 5},
      wave: {stepCount: 500},
      events: {durationSeconds: 600, ratePerSecond: 100, concurrentAgents: 100},
      logs: {totalBytes: 100 * 1024 * 1024, maximumPageBytes: 1024 * 1024},
      thresholds: {
        planningP95Ms: 10_000,
        waveMaximumMs: 30_000,
        eventAcknowledgementP95Ms: 1_000,
        logFirstPageMs: 2_000,
        maximumCrossOrganizationRecords: 0,
        maximumNonPolicyErrorRateExclusive: 0.01,
      },
      remote: {
        planningPath: '/api/v1/control-plane/load-proof/plans',
        wavePath: '/api/v1/control-plane/load-proof/waves',
        eventPath: '/api/executor/v2/load-proof/events',
        logPath: '/api/v1/control-plane/load-proof/logs',
      },
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

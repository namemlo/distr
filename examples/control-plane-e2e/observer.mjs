#!/usr/bin/env node

import {createHash, timingSafeEqual} from 'node:crypto';
import {createServer} from 'node:http';

const digestPattern = /^sha256:[0-9a-f]{64}$/;
const maximumRequestBytes = 256 * 1024;

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

function evidenceChecksum(value) {
  return `sha256:${createHash('sha256').update(stableStringify(value)).digest('hex')}`;
}

function bearerMatches(header, secret) {
  const expected = Buffer.from(`Bearer ${secret}`);
  const actual = Buffer.from(header ?? '');
  return expected.length === actual.length && timingSafeEqual(expected, actual);
}

function send(response, status, body) {
  const encoded = JSON.stringify(body);
  response.writeHead(status, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(encoded),
    'Cache-Control': 'no-store',
  });
  response.end(encoded);
}

async function readJSON(request) {
  const chunks = [];
  let length = 0;
  for await (const chunk of request) {
    length += chunk.length;
    if (length > maximumRequestBytes) {
      throw Object.assign(new Error('request body exceeds limit'), {status: 413, code: 'BODY_TOO_LARGE'});
    }
    chunks.push(chunk);
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString('utf8'));
  } catch {
    throw Object.assign(new Error('request body must be valid JSON'), {status: 400, code: 'INVALID_JSON'});
  }
}

function validateObservation(body, observerId, targetId) {
  if (body?.observerId !== observerId) {
    throw Object.assign(new Error('observer identity does not match registration'), {
      status: 403,
      code: 'OBSERVER_MISMATCH',
    });
  }
  if (body.targetId !== targetId) {
    throw Object.assign(new Error('target identity does not match registration'), {
      status: 403,
      code: 'TARGET_MISMATCH',
    });
  }
  if (!Number.isSafeInteger(body.sequence) || body.sequence < 1) {
    throw Object.assign(new Error('sequence must be a positive integer'), {
      status: 400,
      code: 'INVALID_SEQUENCE',
    });
  }
  if (Number.isNaN(Date.parse(body.observedAt))) {
    throw Object.assign(new Error('observedAt must be an RFC3339 timestamp'), {
      status: 400,
      code: 'INVALID_OBSERVED_AT',
    });
  }
  for (const field of ['releaseDigest', 'configChecksum', 'capabilityChecksum', 'topologyChecksum']) {
    if (!digestPattern.test(body[field] ?? '')) {
      throw Object.assign(new Error(`${field} must be a sha256 digest`), {
        status: 400,
        code: 'INVALID_EVIDENCE',
      });
    }
  }
  if (!body.schemaVersion || body.health !== 'HEALTHY') {
    throw Object.assign(new Error('schemaVersion and healthy status are required'), {
      status: 422,
      code: 'UNHEALTHY_OBSERVATION',
    });
  }
}

export function createObserver({observerId, targetId, sharedSecret}) {
  if (!observerId || !targetId || !sharedSecret) {
    throw new Error('observerId, targetId, and sharedSecret are required');
  }
  let latest;

  async function handle(request, response) {
    const url = new URL(request.url, 'http://observer.invalid');
    if (request.method === 'GET' && url.pathname === '/ready') {
      return send(response, 200, {status: 'ready', observerId, targetId});
    }
    if (!bearerMatches(request.headers.authorization, sharedSecret)) {
      return send(response, 401, {code: 'UNAUTHORIZED', message: 'valid bearer token required'});
    }
    if (request.method === 'POST' && url.pathname === '/v1/observations') {
      const body = await readJSON(request);
      validateObservation(body, observerId, targetId);
      const immutableChecksum = evidenceChecksum(body);
      if (latest && body.sequence < latest.sequence) {
        return send(response, 409, {
          code: 'STALE_OBSERVATION',
          latestSequence: latest.sequence,
        });
      }
      if (latest && body.sequence === latest.sequence) {
        if (latest.evidenceChecksum !== immutableChecksum) {
          return send(response, 409, {
            code: 'OBSERVATION_SEQUENCE_CONFLICT',
            latestSequence: latest.sequence,
          });
        }
        return send(response, 200, latest);
      }
      latest = {...body, evidenceChecksum: immutableChecksum};
      return send(response, 202, latest);
    }
    if (request.method === 'GET' && url.pathname === '/v1/observations/latest') {
      return latest ? send(response, 200, latest) : send(response, 404, {code: 'OBSERVATION_NOT_FOUND'});
    }
    return send(response, 404, {code: 'NOT_FOUND'});
  }

  return createServer((request, response) => {
    handle(request, response).catch((error) => {
      send(response, error.status ?? 500, {
        code: error.code ?? 'INTERNAL_ERROR',
        message: error.status ? error.message : 'request failed',
      });
    });
  });
}

function main() {
  const port = Number.parseInt(process.env.PORT ?? '8080', 10);
  const host = process.env.HOST ?? '127.0.0.1';
  const server = createObserver({
    observerId: process.env.OBSERVER_ID,
    targetId: process.env.TARGET_ID,
    sharedSecret: process.env.OBSERVER_SHARED_SECRET,
  });
  server.listen(port, host, () => {
    console.log(`observer ${process.env.OBSERVER_ID} ready on ${host}:${port}`);
  });
  const shutdown = () => server.close(() => process.exit(0));
  process.once('SIGINT', shutdown);
  process.once('SIGTERM', shutdown);
}

if (process.argv[1] && import.meta.url === new URL(`file:///${process.argv[1].replaceAll('\\', '/')}`).href) {
  main();
}

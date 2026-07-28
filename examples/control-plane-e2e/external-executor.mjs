#!/usr/bin/env node

import {createHash, createHmac, timingSafeEqual} from 'node:crypto';
import {createServer} from 'node:http';

const digestPattern = /^sha256:[0-9a-f]{64}$/;
const maximumRequestBytes = 1024 * 1024;

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

function checksum(value) {
  return `sha256:${createHash('sha256').update(stableStringify(value)).digest('hex')}`;
}

function bearerMatches(header, secret) {
  const expected = Buffer.from(`Bearer ${secret}`);
  const actual = Buffer.from(header ?? '');
  return expected.length === actual.length && timingSafeEqual(expected, actual);
}

function signatureMatches(intent, signature, secret) {
  const expected = Buffer.from(`sha256:${createHmac('sha256', secret).update(stableStringify(intent)).digest('hex')}`);
  const actual = Buffer.from(signature ?? '');
  return expected.length === actual.length && timingSafeEqual(expected, actual);
}

function send(response, status, body, contentType = 'application/json') {
  const encoded = typeof body === 'string' ? body : JSON.stringify(body);
  response.writeHead(status, {
    'Content-Type': contentType,
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

function validateOperation(body, targetId) {
  const required = ['operationId', 'idempotencyKey', 'intent', 'signature'];
  for (const field of required) {
    if (!body?.[field]) {
      throw Object.assign(new Error(`${field} is required`), {status: 400, code: 'INVALID_OPERATION'});
    }
  }
  const intent = body.intent;
  if (intent.schemaVersion !== 'distr.executor-intent/v2') {
    throw Object.assign(new Error('intent schema version is unsupported'), {
      status: 400,
      code: 'INVALID_INTENT',
    });
  }
  for (const field of [
    'tenantId',
    'targetId',
    'taskId',
    'stepId',
    'planId',
    'adapterRevision',
    'resourceKey',
    'issuedAt',
    'expiresAt',
  ]) {
    if (!intent[field]) {
      throw Object.assign(new Error(`intent.${field} is required`), {
        status: 400,
        code: 'INVALID_INTENT',
      });
    }
  }
  if (intent.targetId !== targetId) {
    throw Object.assign(new Error('intent is bound to a different target'), {
      status: 403,
      code: 'TARGET_MISMATCH',
    });
  }
  if (!Number.isSafeInteger(intent.fenceGeneration) || intent.fenceGeneration < 1) {
    throw Object.assign(new Error('intent.fenceGeneration must be a positive integer'), {
      status: 400,
      code: 'INVALID_FENCE',
    });
  }
  if (!digestPattern.test(intent.payload?.releaseDigest ?? '')) {
    throw Object.assign(new Error('release digest is invalid'), {
      status: 400,
      code: 'INVALID_RELEASE_DIGEST',
    });
  }
  if (!digestPattern.test(intent.payload?.configChecksum ?? '')) {
    throw Object.assign(new Error('config checksum is invalid'), {
      status: 400,
      code: 'INVALID_CONFIG_CHECKSUM',
    });
  }
  if (intent.payload?.migration && intent.payload.migration.retrySafe !== true) {
    throw Object.assign(new Error('migration must explicitly be retry-safe'), {
      status: 400,
      code: 'MIGRATION_NOT_RETRY_SAFE',
    });
  }
}

export function createExternalExecutor({executorId, targetId, sharedSecret, maxLogBytes = 64 * 1024}) {
  if (!executorId || !targetId || !sharedSecret) {
    throw new Error('executorId, targetId, and sharedSecret are required');
  }
  const operations = new Map();
  const idempotency = new Map();
  const fences = new Map();

  async function handle(request, response) {
    const url = new URL(request.url, 'http://executor.invalid');
    if (request.method === 'GET' && url.pathname === '/ready') {
      return send(response, 200, {status: 'ready', executorId, targetId});
    }
    if (!bearerMatches(request.headers.authorization, sharedSecret)) {
      return send(response, 401, {code: 'UNAUTHORIZED', message: 'valid bearer token required'});
    }

    if (request.method === 'POST' && url.pathname === '/v1/operations') {
      const body = await readJSON(request);
      validateOperation(body, targetId);
      if (!signatureMatches(body.intent, body.signature, sharedSecret)) {
        return send(response, 401, {
          code: 'INVALID_SIGNATURE',
          message: 'intent signature validation failed',
        });
      }
      const requestChecksum = checksum(body);
      const previousId = idempotency.get(body.idempotencyKey);
      if (previousId) {
        const previous = operations.get(previousId);
        if (previous.requestChecksum !== requestChecksum) {
          return send(response, 409, {
            code: 'IDEMPOTENCY_CONFLICT',
            message: 'idempotency key was reused with different input',
          });
        }
        return send(response, 200, previous.public);
      }

      const currentFence = fences.get(body.intent.resourceKey) ?? 0;
      if (body.intent.fenceGeneration < currentFence) {
        return send(response, 409, {
          code: 'STALE_FENCE',
          currentFenceGeneration: currentFence,
        });
      }
      fences.set(body.intent.resourceKey, body.intent.fenceGeneration);

      const status = body.intent.payload.simulateLongRunning ? 'RUNNING' : 'SUCCEEDED';
      const publicOperation = {
        operationId: body.operationId,
        status,
        targetId,
        fenceGeneration: body.intent.fenceGeneration,
        resultChecksum: checksum({
          operationId: body.operationId,
          releaseDigest: body.intent.payload.releaseDigest,
          configChecksum: body.intent.payload.configChecksum,
          status,
        }),
      };
      const logText = `${body.operationId} ${status} authorization=[REDACTED] signature=[REDACTED]`;
      operations.set(body.operationId, {
        public: publicOperation,
        requestChecksum,
        logs: Buffer.from(logText).subarray(0, maxLogBytes).toString('utf8'),
      });
      idempotency.set(body.idempotencyKey, body.operationId);
      return send(response, 202, publicOperation);
    }

    const match = /^\/v1\/operations\/([^/]+)(?:\/(cancel|logs))?$/.exec(url.pathname);
    if (!match) {
      return send(response, 404, {code: 'NOT_FOUND'});
    }
    const operation = operations.get(decodeURIComponent(match[1]));
    if (!operation) {
      return send(response, 404, {code: 'OPERATION_NOT_FOUND'});
    }
    if (request.method === 'GET' && !match[2]) {
      return send(response, 200, operation.public);
    }
    if (request.method === 'GET' && match[2] === 'logs') {
      return send(response, 200, operation.logs, 'text/plain; charset=utf-8');
    }
    if (request.method === 'POST' && match[2] === 'cancel') {
      if (operation.public.status === 'RUNNING') {
        operation.public = {...operation.public, status: 'CANCELED'};
      }
      return send(response, 200, operation.public);
    }
    return send(response, 405, {code: 'METHOD_NOT_ALLOWED'});
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
  const server = createExternalExecutor({
    executorId: process.env.EXECUTOR_ID,
    targetId: process.env.TARGET_ID,
    sharedSecret: process.env.EXECUTOR_SHARED_SECRET,
    maxLogBytes: Number.parseInt(process.env.MAX_LOG_BYTES ?? '65536', 10),
  });
  server.listen(port, host, () => {
    console.log(`external executor ready on ${host}:${port}`);
  });
  const shutdown = () => server.close(() => process.exit(0));
  process.once('SIGINT', shutdown);
  process.once('SIGTERM', shutdown);
}

if (process.argv[1] && import.meta.url === new URL(`file:///${process.argv[1].replaceAll('\\', '/')}`).href) {
  main();
}

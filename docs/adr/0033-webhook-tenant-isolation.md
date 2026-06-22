# ADR-0033: Webhook Tenant Isolation

## Status

Accepted

## Context

PR-031 introduced webhook idempotent replay by letting Docker agents inspect stored task timeline events before retrying a webhook step. PR-032 added signing-key rotation. The replay path still needed an explicit authorization boundary so an agent would only trust stored events that match the active tenant, task lease, and authenticated agent.

The underlying StepRun event schema already stores organization, lease, and agent identifiers. PR-033 makes those identifiers part of the hidden agent protocol and uses them as replay trust inputs.

## Decision

Agent task lease responses now include `organizationId` and `agentId`. Task timeline, StepRun event, log chunk, and output API payloads also expose `organizationId` so agent-side replay validation can compare stored history with the active lease context.

The hidden agent task timeline endpoint now requires `leaseId`. It resolves the authenticated deployment target from agent auth, keeps the existing organization and deployment target task checks, and reads timeline events through a lease-scoped database query filtered by organization, lease id, and authenticated agent id.

The Docker-agent webhook replay preflight passes the active lease id to the timeline endpoint and rejects stored history with mismatched organization, agent, task, or lease identity before DNS resolution, signing, transport setup, or HTTP requests.

Outbound webhook signatures now include the organization id in the canonical HMAC data. Requests also carry `X-Distr-Tenant-ID`, and the header remains in the reserved `X-Distr-*` namespace so user-provided webhook headers cannot override it.

## Consequences

Webhook replay remains side-effect free, but stored events are no longer trusted unless they match the active tenant and agent boundary.

The hidden timeline endpoint becomes stricter for agents: refreshed manifests and clients include the `leaseId` query parameter, while missing lease ids return a bad request.

Existing public task timeline reads remain unchanged. No database schema migration is required because the required organization, lease, and agent columns already exist.

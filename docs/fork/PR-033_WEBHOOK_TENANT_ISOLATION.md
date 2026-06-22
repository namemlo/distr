# PR-033 - Webhook tenant isolation

PR-033 tightens Docker-agent `distr.webhook` replay and signing boundaries so stored webhook success events are only trusted for the active organization, task lease, and authenticated agent.

## Scope

Included:

- agent task lease API context for `organizationId` and `agentId`
- organization metadata on task timeline, step event, log chunk, and output API payloads
- lease-scoped hidden agent task timeline reads using `leaseId`
- database timeline filtering by organization, lease, and authenticated agent
- replay validation that rejects mismatched organization or agent history before DNS, signing, or HTTP transport
- tenant-bound outbound webhook signatures and `X-Distr-Tenant-ID` request metadata
- reserved-header protection for `X-Distr-Tenant-ID`

Not included:

- database schema changes
- UI changes
- public task timeline behavior changes
- inbound webhook receiver behavior
- action version bump

## Behavior

Docker agents now receive the organization and agent identifiers with each claimed task lease. When a webhook step checks stored timeline history, the agent calls the hidden timeline endpoint with the active lease id. The Hub only returns StepRun events for that organization, that lease, and the authenticated deployment target.

Replay reconstruction validates organization, agent, task, and lease identity before trusting stored webhook success events. If stored history belongs to a different organization or agent, replay fails closed before any outbound side effect can occur.

Outbound webhook signatures now include the organization id in the canonical HMAC data and send `X-Distr-Tenant-ID` as request metadata. User-supplied webhook headers cannot override this metadata.

## Verification

Focused tests cover:

- tenant-bound webhook HMAC signatures and `X-Distr-Tenant-ID`
- agent client `leaseId` timeline requests
- replay rejection for mismatched organization and agent timeline events
- existing idempotent replay behavior after lease scoping

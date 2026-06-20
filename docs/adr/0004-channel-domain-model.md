# ADR-0004: Channel Domain Model

## Status

Accepted for PR-004.

## Context

The roadmap introduces Channels as application-scoped release tracks that select a lifecycle and later select version and source rules. PR-004 is limited to the Channel model, default Channel behavior, CRUD APIs, and administration UI. Version/source rule execution belongs to PR-005.

## Decision

Add an organization-scoped `Channel` table with references to `Application` and `Lifecycle`.

Channels are unique by `(organization_id, application_id, name)`. This allows different applications in the same organization to use the same generic Channel names such as `Stable` or `Preview`, while preventing duplicate names inside one application's Channel set.

Only one default Channel is allowed per organization/application. The repository creates a `Stable` default idempotently for applications that have no Channels when Channels are listed and at least one Lifecycle exists.

When a non-default Channel is moved to an application without a default, the moved Channel becomes the default inside the update transaction. Moving a non-default Channel to an application with a default preserves the requested non-default state. Moving an existing default Channel to another application is rejected.

Delete operations lock the Channel row before validating `is_default` and deleting the row. This preserves the default invariant when delete and promotion operations race.

The API is guarded by the existing `channels` experimental feature flag. The frontend route and sidebar require `environments`, `lifecycles`, and `channels` because the Channel editor uses lifecycle selectors.

## Consequences

- Cross-organization application and lifecycle references are rejected as not found.
- Default Channels cannot be deleted through PR-004 APIs.
- Existing default Channels cannot be moved to another application.
- Lifecycle deletion is restricted while Channels reference that lifecycle.
- Application deletion cascades its Channels with the application.
- PR-004 remains generic and does not add release promotion, version validation, rule evaluation, deployment planning, or execution behavior.

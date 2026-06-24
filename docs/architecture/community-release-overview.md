# Community Release Architecture Overview

The community release keeps Distr's existing customer deployment foundation and adds release-management
primitives behind experimental flags.

## Core Runtime Boundaries

- Hub owns desired state, API validation, authorization, planning, task records, and audit metadata.
- PostgreSQL stores release, process, variable, plan, task, lease, compatibility, and authority records.
- Agents poll for scoped work, advertise capabilities, execute typed actions, and send redacted step events.
- The Angular UI is a client of documented API routes and does not bypass server authorization.
- Config as Code validates documents and authority state but does not sync or apply repository content in this
  release.

## Release Execution Path

1. CI or a user publishes a release bundle.
2. A deployment process revision and variable snapshot are selected.
3. The planner produces a deployment plan and checksum.
4. Task creation records immutable work.
5. Agents acquire leases and execute only supported typed actions.
6. Step events and logs are redacted before storage and display.
7. Timeline and compatibility views read from task-backed history and legacy projections.

## Non-Goals

- No provider-specific orchestration is embedded in core.
- No arbitrary script console is added.
- No secret values are stored in demo fixtures or Config as Code documents.
- No existing direct deployment behavior is replaced.

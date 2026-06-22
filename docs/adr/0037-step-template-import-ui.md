# ADR-0037: Step Template Import UI

## Status

Accepted

## Context

Deployment Process revisions can already reference Step Template versions, but admins did not yet have a governed place to discover and install reusable template definitions. Without a tenant-installed template catalog, process authors must manually know template version ids and default inputs.

The fork roadmap calls for Step Template import/install before output variables and conditions, so this PR establishes the catalog and install boundary without expanding process execution semantics.

## Decision

Add organization-scoped `StepTemplate` and `StepTemplateVersion` tables plus feature-flagged vendor admin APIs for list, read, and import. Imports validate JSON schema shape and default input compatibility against the existing action registry before persisting an installed template.

Add a feature-flagged Angular Step Templates page that presents a small built-in catalog, previews defaults, imports a catalog item into the current organization, and lists installed templates with their latest action and version metadata.

The first implementation stores imported template definitions directly in the Hub database. Source metadata is retained through `source_type` and `source_ref`, but this PR does not pull remote OCI artifacts or manage upgrades.

## Consequences

Admins can install and inspect Step Templates in a tenant-scoped, validated way before later PRs connect them more deeply into process authoring and execution.

Duplicate source installs are rejected per organization to keep catalog status deterministic.

External marketplace sync, deletion, upgrade, and deployment-process picker ergonomics remain future work.

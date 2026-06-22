# PR-037 - Step Template import UI

PR-037 adds the first Step Template catalog and install experience. Vendor admins can inspect built-in template definitions, install them into their organization, and review installed template versions for use by deployment process steps.

## Scope

Included:

- organization-scoped Step Template and Step Template Version schema
- repository import, list, and read operations with duplicate-install protection
- JSON schema and default input validation through the action registry
- feature-flagged Step Template API endpoints
- Angular service, route, sidebar link, catalog list, preview dialog, install flow, and installed-template list
- `step_templates` experimental feature flag registration

Not included:

- external marketplace sync
- OCI pull/download execution
- editing installed templates
- deleting or upgrading installed templates
- deployment process editor template picker
- agent protocol changes

## API

Feature-flagged vendor admin endpoints:

- `GET /api/v1/step-templates`
- `GET /api/v1/step-templates/{stepTemplateId}`
- `POST /api/v1/step-templates/import`

Imports require a supported source type, source reference, template name, version, action type, execution location, object-shaped schemas, and action-registry-compatible default inputs.

## UI

The Step Templates page is available to vendor admins when `environments`, `lifecycles`, `channels`, `deployment_processes`, and `step_templates` are enabled.

The page shows:

- built-in catalog templates
- installed status for matching catalog entries
- preview details and default input bindings
- one-click install for catalog templates
- installed template versions and source references

## Verification

Focused tests cover:

- repository import/list/read behavior
- duplicate install rejection
- invalid schema and default-input rejection
- migration schema shape
- API list/read/import handlers and integration flow
- Angular service requests
- Angular component load, preview, error, and install states

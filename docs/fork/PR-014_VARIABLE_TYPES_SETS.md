# PR-014 Variable Types and Sets

## Scope

PR-014 adds the generic Variable Set model for typed, organization-scoped variables.

Included:

- `scoped_variables_v2` experimental feature flag.
- Organization-scoped Variable Set CRUD.
- Ordered Variable Sets with optional application links.
- Typed variables in a set:
  - string
  - number
  - boolean
  - JSON
  - secret reference
  - account reference
  - certificate reference
- Non-secret default values stored as typed JSON.
- Secret references stored as references only, with safe secret-key metadata returned in responses.
- Metadata-only account and certificate references.
- Feature-flagged backend API, repository, mapping, handlers, routes, and Angular admin UI.
- API, handler, repository, mapping, migration, and Angular tests.

Excluded:

- Scoped variable resolution, precedence, explanation, prompted-value collection, or preview APIs.
- Variable snapshots or drift detection.
- Runtime secret resolution or a new secrets manager.
- Plaintext credential storage in Variable Sets.
- Release promotion, deployment planning, approvals, retention, execution, notification, task, or agent changes.
- Provider-specific or adopter-specific variable logic.

Those features remain PR-015 or later roadmap work.

## Feature Flag

Variable Set APIs and UI are hidden unless:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=scoped_variables_v2
```

The Angular route and sidebar link are also gated by `scoped_variables_v2`.

Existing experimental feature flags are preserved.

## Database

Migration `117_variable_sets` adds:

- `VariableSet`
- `VariableSetApplication`
- `Variable`

`VariableSet` rows are scoped to an organization and ordered by `sort_order`, `name`, and `id`.

Names are unique per organization:

```text
(organization_id, name)
```

`VariableSetApplication` links Variable Sets to Applications in the same organization through composite foreign keys. Cross-organization application references are rejected.

`Variable` rows are scoped through their parent Variable Set. Variable keys are unique per set. Type checks enforce that non-reference variables store JSON values matching the declared type, while reference variables do not store inline values.

Secret references use a composite foreign key to `Secret(id, organization_id)` with `ON DELETE RESTRICT`, so referenced organization secrets cannot be deleted while a Variable references them.

The down migration drops the PR-014 tables and removes the composite Secret uniqueness constraint added for the new foreign key.

## API

Feature-flagged endpoints:

```http
GET    /api/v1/variable-sets
POST   /api/v1/variable-sets
GET    /api/v1/variable-sets/{variableSetId}
PUT    /api/v1/variable-sets/{variableSetId}
DELETE /api/v1/variable-sets/{variableSetId}
```

Create/update request shape:

```json
{
  "name": "Runtime Defaults",
  "description": "Shared application variables",
  "sortOrder": 10,
  "applicationIds": ["00000000-0000-0000-0000-000000000000"],
  "variables": [
    {
      "key": "api_url",
      "type": "string",
      "defaultValue": "https://example.test"
    },
    {
      "key": "api_token",
      "type": "secret_reference",
      "referenceId": "00000000-0000-0000-0000-000000000000"
    }
  ]
}
```

Validation:

- trims Variable Set names and Variable keys before persistence
- rejects empty names and keys
- rejects negative sort orders
- rejects duplicate Variable Set names within an organization
- rejects duplicate trimmed variable keys within a set
- rejects unsupported variable types
- validates typed default values
- rejects inline defaults on reference variables
- validates same-organization Application links
- validates same-organization organization-level Secret references
- returns `404 Not Found` for missing or cross-organization references
- returns `403 Forbidden` while `scoped_variables_v2` is disabled

Secret reference responses include the referenced secret ID and key name only. Secret values are never returned by Variable Set APIs.

## UI

The Angular administration UI adds a feature-flagged `/variable-sets` page for vendor admins.

The page supports:

- listing, filtering, creating, editing, and deleting Variable Sets
- selecting organization Applications linked to a Variable Set
- editing typed variables in each set
- selecting organization-level Secret references without exposing values
- loading, empty, validation-error, API-error, and confirmation states

## Compatibility

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, deployment target, deployment, release-name, and agent behavior is unchanged.

Existing secret behavior is preserved except that organization secrets referenced by Variable Sets are protected from deletion with a conflict response.

PR-014 adds no variable resolution, deployment planning, execution, or agent protocol changes.

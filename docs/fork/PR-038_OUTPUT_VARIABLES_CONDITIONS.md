# PR-038 - Output variables and conditions

PR-038 formalizes the first restricted condition language for deployment process steps and tightens step output keys so later workflow logic can safely reference recorded outputs.

## Scope

Included:

- restricted side-effect-free condition parser and evaluator
- support for `always()`, `success()`, `failure()`, `environment.isProduction`, channel comparisons, variable comparisons, and output comparisons
- condition syntax validation during deployment process revision creation
- output-reference validation against known step keys
- condition output references included in cycle detection
- deployment plan blocker issue for invalid conditions found in immutable snapshots
- stable step output name validation for API and repository writes
- sensitive or redacted outputs excluded from condition evaluation context

Not included:

- new database tables or migrations
- general-purpose expression language
- arithmetic, boolean chaining, regex matching, or scripting
- changing Docker or Kubernetes agent execution protocols
- changing webhook, policy, or runtime isolation behavior
- UI condition builder

## Condition Language

Accepted examples:

- `always()`
- `success()`
- `failure()`
- `environment.isProduction`
- `channel == "Stable"`
- `variable("Feature.Enabled") == "true"`
- `output("prepare", "statusCode") == 200`

Only equality and inequality comparisons are accepted for channel, variable, and output operands. Output references must point to existing steps and use stable output names; output-reference dependencies participate in cycle validation.

## Output Variables

Step outputs keep the existing `StepRunOutput` storage from the step-event timeline. PR-038 tightens output names to the stable identifier form:

```text
[A-Za-z_][A-Za-z0-9_.-]*
```

Sensitive or redacted output values remain unavailable to condition evaluation.

## Verification

Focused tests cover:

- accepted and rejected condition expressions
- output reference extraction
- deterministic condition evaluation
- sensitive output exclusion
- deployment process condition validation
- output-reference cycle detection
- API/repository output name validation
- deployment plan blocker handling for invalid snapshot conditions

# ADR-0038: Output Variables and Conditions

## Status

Accepted

## Context

Deployment process steps already carry a condition field and step events already persist bounded `StepRunOutput` rows. The missing boundary is a small, deterministic condition language that can validate conditions before process publication and safely reference output variables without exposing sensitive data.

The roadmap explicitly warns against embedding a general-purpose programming language in phase one. Conditions must be predictable, side-effect free, and tenant-local.

## Decision

Introduce `internal/conditions` with a restricted parser and evaluator. The accepted forms are limited to built-in functions, environment production checks, channel comparisons, variable comparisons, and output comparisons.

Deployment process revision validation now rejects invalid condition syntax, rejects output references to unknown steps, and treats condition output references as dependencies for cycle detection.

Deployment plan creation also validates conditions from immutable snapshots and records an `invalid_step_condition` blocker if old or imported data contains invalid syntax.

Step event output names are constrained to stable identifiers so output references remain deterministic. Sensitive and redacted outputs are not exposed to condition evaluation.

## Consequences

Process authors get deterministic validation before a revision can be accepted.

Future execution logic can use the same condition package without introducing a second expression language.

This PR does not change agent protocols, step execution ordering, or runtime orchestration behavior.

# PR-072: Campaign Scheduler and Thresholds

## Scope

PR-072 adds the persisted runtime for immutable deployment campaigns without
changing v1 deployment execution. Migration 154 introduces campaign, wave, and
member runs plus append-only prerequisite and threshold evaluations.

The runtime is deterministic: pending members are admitted by
`(wave_order, member_order, deployment_plan_id)`. Every mutation is protected
by a monotonically increasing lease fencing token. A repeated tick is harmless
because only a `PENDING` member can become `ADMITTED`.

## State and safety

The state machine accepts the validated path:

`DRAFT -> VALIDATED -> AWAITING_APPROVAL -> SCHEDULED -> RUNNING -> COMPLETED`

Creating a run only instantiates the immutable run/wave/member snapshot in
`DRAFT`. Each later state is an explicit optimistic transition through the
registered `/{campaignRunId}/transitions` route with actor, reason, expected
version, and timestamp evidence. Run creation never manufactures validation,
approval, scheduling, or start evidence.

Running campaigns can pause, fail, or cancel. Paused campaigns can resume,
fail, or cancel. State changes use an expected version and append evidence to
the run. Paused or terminal runs block new admission.

Wave bake durations must be non-negative and non-decreasing. Threshold
evaluation and a breached-threshold pause share one transaction. Likewise,
the exact observation evidence for every prerequisite and the resulting member
admission or fail-closed pause share one transaction.

## Trusted observation seam

`internal/campaigns.CampaignObservationVerifier` is the compile-safe PR-077
integration point:

```go
type CampaignObservationVerifier interface {
    VerifyCampaignObservation(context.Context, uuid.UUID, uuid.UUID, string) error
}
```

The arguments are organization ID, observation ID, and runtime-state checksum. The default
`UnwiredCampaignObservationVerifier` returns
`ErrCampaignObservationVerifierUnavailable`. The scheduler records an
unmatched prerequisite evaluation with no trusted actual binding, pauses the
campaign, and admits nothing. It never substitutes a newer observation or
rebinds the frozen expected checksum.

PR-077 must provide the organization-scoped implementation. The scheduler
persists the stable runtime-state checksum used for comparison together with
the exact observation ID; it does not use the observation evidence-envelope
checksum as runtime state. Until the resolver/verifier is available, every
trusted-observation prerequisite fails closed.

## Replay seams

This change was built on the deliberately synthetic PR-069 base. During stack
replay:

- migration 154 must follow PR-070 migration 152 and PR-071 migration 153;
- `DeploymentCampaignRevision` and `DeploymentCampaignWave` foreign-key column
  names must be checked against the final PR-071 schema;
- PR-071's campaign API and type files must be combined with the feature-local
  runtime representations in this change;
- run, wave-run, and member-run rows are instantiated atomically from the
  immutable revision through the registered campaign-run route;
- the authenticated route is registered behind `operator_control_plane_v2`;
- migration 154 protects runtime rows from direct deletion/truncation and
  protects evaluation evidence from update/delete/truncate by reusing PR-071's
  hardened organization-retention proof (reason, operation ID, cascade depth,
  and deleted organization).

## Compatibility

No existing route, deployment task, agent protocol, or v1 execution behavior is
changed. Campaign scheduling remains unavailable unless the earlier campaign
revision stack is present and the experimental control-plane feature is wired.

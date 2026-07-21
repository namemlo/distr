package db

import (
	"context"
	"errors"

	"github.com/distr-sh/distr/internal/types"
)

type ControlPlaneAuditAppendHook interface {
	AppendControlPlaneAuditEvent(context.Context, types.ControlPlaneAuditEventInput) error
}

type ControlPlaneAuditAppendHookFunc func(context.Context, types.ControlPlaneAuditEventInput) error

func (hook ControlPlaneAuditAppendHookFunc) AppendControlPlaneAuditEvent(
	ctx context.Context,
	input types.ControlPlaneAuditEventInput,
) error {
	return hook(ctx, input)
}

var DirectControlPlaneAuditAppendHook ControlPlaneAuditAppendHook = ControlPlaneAuditAppendHookFunc(
	func(ctx context.Context, input types.ControlPlaneAuditEventInput) error {
		_, err := AppendControlPlaneAuditEventInCurrentBoundary(ctx, input)
		return err
	},
)

func RecordControlPlaneAuditMutation(
	ctx context.Context,
	hook ControlPlaneAuditAppendHook,
	input types.ControlPlaneAuditEventInput,
) error {
	if hook == nil {
		return errors.New("control-plane audit append hook is required")
	}
	if err := validateControlPlaneAuditEventInput(input); err != nil {
		return err
	}
	return hook.AppendControlPlaneAuditEvent(ctx, input)
}

type controlPlaneAuditTxRunner func(context.Context, func(context.Context) error) error

func RunControlPlaneAuditedMutation(
	ctx context.Context,
	hook ControlPlaneAuditAppendHook,
	mutation func(context.Context) (types.ControlPlaneAuditEventInput, error),
) error {
	return runControlPlaneAuditedMutation(ctx, RunTx, hook, mutation)
}

func runControlPlaneAuditedMutation(
	ctx context.Context,
	runTx controlPlaneAuditTxRunner,
	hook ControlPlaneAuditAppendHook,
	mutation func(context.Context) (types.ControlPlaneAuditEventInput, error),
) error {
	if runTx == nil || mutation == nil {
		return errors.New("control-plane audited mutation boundary is not configured")
	}
	return runTx(ctx, func(ctx context.Context) error {
		input, err := mutation(ctx)
		if err != nil {
			return err
		}
		return RecordControlPlaneAuditMutation(ctx, hook, input)
	})
}

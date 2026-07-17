package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProtectedWriteErrorMappersTreatRestrictViolationAsConflict(t *testing.T) {
	t.Parallel()

	mappers := map[string]func(string, error) error{
		"channel":            mapChannelWriteError,
		"deployment process": mapDeploymentProcessWriteError,
		"release bundle":     mapReleaseBundleWriteError,
	}
	for name, mapper := range mappers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pgError := &pgconn.PgError{Code: pgerrcode.RestrictViolation}

			err := mapper("update", fmt.Errorf("wrapped database error: %w", pgError))

			if !errors.Is(err, apierrors.ErrConflict) {
				t.Fatalf("expected restrict violation to map to conflict, got %v", err)
			}
		})
	}
}

func TestProtectedWriteErrorMappersKeepExistingSemantics(t *testing.T) {
	t.Parallel()

	mappers := map[string]func(string, error) error{
		"channel":        mapChannelWriteError,
		"release bundle": mapReleaseBundleWriteError,
	}
	for name, mapper := range mappers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("foreign key violation remains conflict", func(t *testing.T) {
				err := mapper("update", &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation})
				if !errors.Is(err, apierrors.ErrConflict) {
					t.Fatalf("expected foreign key violation to map to conflict, got %v", err)
				}
			})

			t.Run("unique violation remains already exists", func(t *testing.T) {
				err := mapper("update", &pgconn.PgError{Code: pgerrcode.UniqueViolation})
				if !errors.Is(err, apierrors.ErrAlreadyExists) {
					t.Fatalf("expected unique violation to map to already exists, got %v", err)
				}
			})

			t.Run("other integrity errors remain raw", func(t *testing.T) {
				pgError := &pgconn.PgError{Code: pgerrcode.CheckViolation}
				err := mapper("update", pgError)
				if !errors.Is(err, pgError) {
					t.Fatalf("expected check violation to remain in the error chain, got %v", err)
				}
				if errors.Is(err, apierrors.ErrConflict) || errors.Is(err, apierrors.ErrAlreadyExists) {
					t.Fatalf("expected check violation to remain unclassified, got %v", err)
				}
			})

			t.Run("non PostgreSQL errors remain raw", func(t *testing.T) {
				rawError := errors.New("unexpected database failure")
				err := mapper("update", rawError)
				if !errors.Is(err, rawError) {
					t.Fatalf("expected raw error to remain in the error chain, got %v", err)
				}
			})
		})
	}
}

func TestMapRunbookWriteErrorKeepsReferenceAndRestrictSemanticsDistinct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want error
	}{
		{
			name: "foreign key reference remains not found",
			code: pgerrcode.ForeignKeyViolation,
			want: apierrors.ErrNotFound,
		},
		{
			name: "restrict violation is conflict",
			code: pgerrcode.RestrictViolation,
			want: apierrors.ErrConflict,
		},
		{
			name: "unique violation remains already exists",
			code: pgerrcode.UniqueViolation,
			want: apierrors.ErrAlreadyExists,
		},
		{
			name: "check violation remains bad request",
			code: pgerrcode.CheckViolation,
			want: apierrors.ErrBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := mapRunbookWriteError("write", &pgconn.PgError{Code: test.code})
			if !errors.Is(err, test.want) {
				t.Fatalf("expected SQLSTATE %s to map to %v, got %v", test.code, test.want, err)
			}
		})
	}
}

func TestMapDeploymentProcessStepReferenceWriteErrorKeepsReferenceAndRestrictSemanticsDistinct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want error
	}{
		{
			name: "foreign key reference remains not found",
			code: pgerrcode.ForeignKeyViolation,
			want: apierrors.ErrNotFound,
		},
		{
			name: "restrict violation is conflict",
			code: pgerrcode.RestrictViolation,
			want: apierrors.ErrConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := mapDeploymentProcessStepReferenceWriteError(
				"write reference",
				&pgconn.PgError{Code: test.code},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("expected SQLSTATE %s to map to %v, got %v", test.code, test.want, err)
			}
		})
	}
}

func TestIsProtectedReferenceViolationRecognizesOnlyProtectedReferenceCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "foreign key violation",
			err:  &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation},
			want: true,
		},
		{
			name: "restrict violation",
			err:  &pgconn.PgError{Code: pgerrcode.RestrictViolation},
			want: true,
		},
		{
			name: "wrapped restrict violation",
			err: fmt.Errorf(
				"wrapped database error: %w",
				&pgconn.PgError{Code: pgerrcode.RestrictViolation},
			),
			want: true,
		},
		{
			name: "unrelated integrity violation",
			err:  &pgconn.PgError{Code: pgerrcode.CheckViolation},
			want: false,
		},
		{
			name: "raw error",
			err:  errors.New("unexpected database failure"),
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isProtectedReferenceViolation(test.err); got != test.want {
				t.Fatalf("isProtectedReferenceViolation() = %t, want %t", got, test.want)
			}
		})
	}
}

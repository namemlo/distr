package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestAuthorizationPostgreSQLConcurrentRevocationsAndMembershipReentry(
	t *testing.T,
) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 148)
	organizationID, userID := createAuthorizationPostgreSQLPrincipal(t, ctx)
	g := NewWithT(t)
	g.Expect(BackfillBuiltInAuthorization(ctx, organizationID)).To(Succeed())

	var adminRoleID uuid.UUID
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id
		FROM RoleDefinition
		WHERE organization_id = @organizationID
		  AND role_key = 'legacy.admin'`,
		pgx.NamedArgs{"organizationID": organizationID},
	).Scan(&adminRoleID)).To(Succeed())

	firstBinding := createAuthorizationPostgreSQLBinding(
		t,
		ctx,
		organizationID,
		userID,
		adminRoleID,
	)
	var oldMembershipCreatedAt time.Time
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT created_at
		FROM Organization_UserAccount
		WHERE organization_id = @organizationID
		  AND user_account_id = @userID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"userID":         userID,
		},
	).Scan(&oldMembershipCreatedAt)).To(Succeed())
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		DELETE FROM Organization_UserAccount
		WHERE organization_id = @organizationID
		  AND user_account_id = @userID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"userID":         userID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO Organization_UserAccount (
		  organization_id,
		  user_account_id,
		  user_role,
		  created_at
		) VALUES (
		  @organizationID,
		  @userID,
		  'admin',
		  @createdAt
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"userID":         userID,
			"createdAt":      oldMembershipCreatedAt.Add(time.Second),
		},
	)
	g.Expect(err).NotTo(HaveOccurred())

	grants, err := ListAuthorizationAccessGrants(
		ctx,
		organizationID,
		userID,
		time.Now().UTC(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(grants).To(BeEmpty(), firstBinding.ID.String())

	secondBinding := createAuthorizationPostgreSQLBinding(
		t,
		ctx,
		organizationID,
		userID,
		adminRoleID,
	)
	revokeAt := time.Now().UTC().Add(time.Minute)
	var waitGroup sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			errors <- RevokeAuthorizationRoleBinding(
				ctx,
				&types.RoleBindingRevision{
					OrganizationID: organizationID,
					RoleBindingID:  secondBinding.ID,
					EffectiveFrom:  revokeAt,
					ActorUserID:    userID,
					Reason:         "concurrent revocation proof",
				},
			)
		}()
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		g.Expect(err).NotTo(HaveOccurred())
	}

	var revisions []int64
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT revision
		FROM RoleBindingRevision
		WHERE organization_id = @organizationID
		  AND role_binding_id = @bindingID
		ORDER BY revision`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"bindingID":      secondBinding.ID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	revisions, err = pgx.CollectRows(rows, pgx.RowTo[int64])
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(revisions).To(Equal([]int64{1, 2, 3}))

	before, err := ListAuthorizationAccessGrants(
		ctx,
		organizationID,
		userID,
		revokeAt.Add(-time.Nanosecond),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(before).NotTo(BeEmpty())
	at, err := ListAuthorizationAccessGrants(
		ctx,
		organizationID,
		userID,
		revokeAt,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(at).To(BeEmpty())
}

func TestAuthorizationPostgreSQLDownLockSerializesWriterBeforeGuard(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 148)
	organizationID := createDeploymentRegistryOrganization(t, ctx)
	tx, err := pool.Begin(ctx)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = tx.Rollback(context.Background())
	}()
	_, err = tx.Exec(ctx, `
		INSERT INTO RoleDefinition (
		  organization_id,
		  role_key,
		  display_name,
		  built_in,
		  revision
		) VALUES (
		  @organizationID,
		  'down-lock-proof',
		  'Down Lock Proof',
		  false,
		  1
		)`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	g.Expect(err).NotTo(HaveOccurred())

	downSQL, err := os.ReadFile(filepath.Join(
		"..",
		"migrations",
		"sql",
		"148_scoped_authorization_enrollment.down.sql",
	))
	g.Expect(err).NotTo(HaveOccurred())
	result := make(chan error, 1)
	go func() {
		_, executionErr := pool.Exec(context.Background(), string(downSQL))
		result <- executionErr
	}()

	select {
	case early := <-result:
		t.Fatalf("down migration bypassed the writer lock: %v", early)
	case <-time.After(100 * time.Millisecond):
	}
	g.Expect(tx.Commit(ctx)).To(Succeed())
	select {
	case downErr := <-result:
		g.Expect(downErr).To(HaveOccurred())
		g.Expect(downErr.Error()).To(
			ContainSubstring("refusing migration 148 rollback"),
		)
	case <-time.After(5 * time.Second):
		t.Fatal("down migration did not resume after writer commit")
	}
}

func createAuthorizationPostgreSQLPrincipal(
	t *testing.T,
	ctx context.Context,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	organizationID := createDeploymentRegistryOrganization(t, ctx)
	var userID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO UserAccount (email, name)
		VALUES (@email, 'Authorization Test User')
		RETURNING id`,
		pgx.NamedArgs{
			"email": "authorization-" +
				strings.ReplaceAll(uuid.NewString(), "-", "") +
				"@example.com",
		},
	).Scan(&userID)
	if err != nil {
		t.Fatalf("create authorization test user: %v", err)
	}
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO Organization_UserAccount (
		  organization_id,
		  user_account_id,
		  user_role
		) VALUES (
		  @organizationID,
		  @userID,
		  'admin'
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"userID":         userID,
		},
	)
	if err != nil {
		t.Fatalf("create authorization test membership: %v", err)
	}
	return organizationID, userID
}

func createAuthorizationPostgreSQLBinding(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
	userID uuid.UUID,
	roleID uuid.UUID,
) types.RoleBinding {
	t.Helper()
	binding := types.RoleBinding{
		OrganizationID:   organizationID,
		RoleDefinitionID: roleID,
		PrincipalKind:    types.AuthorizationPrincipalUser,
		PrincipalID:      userID,
		Scope: types.ScopeRef{
			Kind: types.PermissionScopeOrganization,
			ID:   organizationID,
		},
		EffectiveFrom:   time.Now().UTC().Add(-time.Minute),
		Reason:          "postgres integration proof",
		CreatedByUserID: &userID,
	}
	if err := CreateAuthorizationRoleBinding(ctx, &binding); err != nil {
		t.Fatalf("create authorization role binding: %v", err)
	}
	return binding
}

package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestSampleRetirementPreviewRequestRequiresAnExactEvidenceBackedAllowlist(t *testing.T) {
	t.Parallel()

	valid := validSampleRetirementPreviewRequest()
	g := NewWithT(t)
	g.Expect(valid.Validate()).To(Succeed())

	tests := []struct {
		name   string
		mutate func(*api.SampleRetirementPreviewRequest)
	}{
		{
			name: "wildcard selector",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Selector.Wildcard = "*"
			},
		},
		{
			name: "name pattern selector",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Selector.NamePattern = "demo-*"
			},
		},
		{
			name: "age selector",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Selector.OlderThan = new(time.Now().UTC())
			},
		},
		{
			name: "empty allowlist",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Items = nil
			},
		},
		{
			name: "nil subject id",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Items[0].SubjectID = uuid.Nil
			},
		},
		{
			name: "unsupported subject type",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Items[0].SubjectType = "organization"
			},
		},
		{
			name: "missing ownership marker",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Items[0].OwnershipMarker = ""
			},
		},
		{
			name: "invalid ownership checksum",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Items[0].OwnershipChecksum = "sha256:not-canonical"
			},
		},
		{
			name: "missing expected checksum",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Items[0].ExpectedChecksum = ""
			},
		},
		{
			name: "duplicate exact id",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.Items = append(request.Items, request.Items[0])
			},
		},
		{
			name: "missing backup reference",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.BackupReference = ""
			},
		},
		{
			name: "invalid restore proof checksum",
			mutate: func(request *api.SampleRetirementPreviewRequest) {
				request.RestoreProofChecksum = "SHA256:" + strings.Repeat("a", 64)
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validSampleRetirementPreviewRequest()
			test.mutate(&request)
			NewWithT(t).Expect(request.Validate()).NotTo(Succeed())
		})
	}
}

func TestSampleRetirementRouterRegistersStablePublicContracts(t *testing.T) {
	t.Parallel()

	base := chi.NewRouter()
	openAPIRouter := chiopenapi.NewRouter(base)
	openAPIRouter.Route(
		"/api/v1/sample-retirements",
		SampleRetirementRouterWithService(sampleRetirementServiceStub{}),
	)
	openAPIRouter.Route(
		"/api/v1/sample-retirement-evidence",
		SampleRetirementEvidenceRouterWithService(sampleRetirementEvidenceServiceStub{}),
	)
	routes := map[string]bool{}
	err := chi.Walk(base, func(
		method, route string,
		_ http.Handler,
		_ ...func(http.Handler) http.Handler,
	) error {
		routes[method+" "+route] = true
		return nil
	})

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(routes).To(HaveKey("POST /api/v1/sample-retirements/preview"))
	g.Expect(routes).To(HaveKey("GET /api/v1/sample-retirements/{sampleRetirementId}"))
	g.Expect(routes).To(HaveKey(
		"POST /api/v1/sample-retirements/{sampleRetirementId}/approval-requests",
	))
	g.Expect(routes).To(HaveKey("POST /api/v1/sample-retirements/{sampleRetirementId}/apply"))
	g.Expect(routes).To(HaveKey("POST /api/v1/sample-retirements/{sampleRetirementId}/verify"))
	g.Expect(routes).To(HaveKey("POST /api/v1/sample-retirement-evidence/ownership"))
	g.Expect(routes).To(HaveKey("POST /api/v1/sample-retirement-evidence/recovery"))
}

func TestSampleRetirementEvidenceHandlersInjectAuthenticatedIdentity(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	authInfo := testChannelAuth()
	verifiedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	subjectID := uuid.New()
	sourceID := uuid.New()
	var gotOwnership types.SampleRetirementOwnershipEvidenceRegistrationInput
	var gotRecovery types.SampleRetirementRecoveryEvidenceRegistrationInput
	ownership := types.SampleRetirementOwnershipEvidence{
		ID:             uuid.New(),
		OrganizationID: authInfo.orgID,
		SubjectType:    types.SampleRetirementSubjectApplication,
		SubjectID:      subjectID,
	}
	recovery := types.SampleRetirementRecoveryEvidence{
		ID:             uuid.New(),
		OrganizationID: authInfo.orgID,
		EvidenceKind:   types.SampleRetirementRecoveryEvidenceBackup,
		SourceID:       sourceID,
	}
	service := sampleRetirementEvidenceServiceStub{
		registerOwnership: func(
			_ context.Context,
			input types.SampleRetirementOwnershipEvidenceRegistrationInput,
		) (*types.SampleRetirementOwnershipEvidence, error) {
			gotOwnership = input
			return &ownership, nil
		},
		registerRecovery: func(
			_ context.Context,
			input types.SampleRetirementRecoveryEvidenceRegistrationInput,
		) (*types.SampleRetirementRecoveryEvidence, error) {
			gotRecovery = input
			return &recovery, nil
		},
	}

	ownershipRecorder := httptest.NewRecorder()
	ownershipRequest := sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirement-evidence/ownership",
		`{"subjectType":"application","subjectId":"`+subjectID.String()+
			`","ownershipMarker":"tutorial-fixture","ownershipChecksum":"`+
			canonicalChecksum("a")+`","sourceReference":"evidence://ownership/tutorial",`+
			`"sourceChecksum":"`+canonicalChecksum("b")+`"}`,
		authInfo,
	)
	registerSampleRetirementOwnershipEvidenceHandler(service).
		ServeHTTP(ownershipRecorder, ownershipRequest)

	g.Expect(ownershipRecorder.Code).To(Equal(http.StatusCreated))
	g.Expect(gotOwnership.OrganizationID).To(Equal(authInfo.orgID))
	g.Expect(gotOwnership.RecordedByUserAccountID).To(Equal(authInfo.userID))
	g.Expect(gotOwnership.SubjectID).To(Equal(subjectID))
	g.Expect(ownershipRecorder.Body.String()).To(ContainSubstring(ownership.ID.String()))

	recoveryRecorder := httptest.NewRecorder()
	recoveryRequest := sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirement-evidence/recovery",
		`{"evidenceKind":"backup","reference":"s3://immutable-backups/sample-job",`+
			`"checksum":"`+canonicalChecksum("c")+`","sourceKind":"backup_manifest",`+
			`"sourceId":"`+sourceID.String()+`","sourceChecksum":"`+
			canonicalChecksum("d")+`","verifiedAt":"`+verifiedAt.Format(time.RFC3339)+`"}`,
		authInfo,
	)
	registerSampleRetirementRecoveryEvidenceHandler(service).
		ServeHTTP(recoveryRecorder, recoveryRequest)

	g.Expect(recoveryRecorder.Code).To(Equal(http.StatusCreated))
	g.Expect(gotRecovery.OrganizationID).To(Equal(authInfo.orgID))
	g.Expect(gotRecovery.VerifiedByUserAccountID).To(Equal(authInfo.userID))
	g.Expect(gotRecovery.SourceID).To(Equal(sourceID))
	g.Expect(gotRecovery.VerifiedAt).To(Equal(verifiedAt))
	g.Expect(recoveryRecorder.Body.String()).To(ContainSubstring(recovery.ID.String()))
}

func TestSampleRetirementEvidenceHandlersRejectInvalidAndConflictingRegistrations(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	called := false
	service := sampleRetirementEvidenceServiceStub{
		registerOwnership: func(
			context.Context,
			types.SampleRetirementOwnershipEvidenceRegistrationInput,
		) (*types.SampleRetirementOwnershipEvidence, error) {
			called = true
			return nil, apierrors.ErrAlreadyExists
		},
	}
	recorder := httptest.NewRecorder()
	request := sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirement-evidence/ownership",
		`{"subjectType":"application","subjectId":"`+uuid.NewString()+
			`","ownershipMarker":"bad\nmarker","ownershipChecksum":"`+
			canonicalChecksum("a")+`","sourceReference":"evidence://ownership/tutorial",`+
			`"sourceChecksum":"`+canonicalChecksum("b")+`"}`,
		testChannelAuth(),
	)
	registerSampleRetirementOwnershipEvidenceHandler(service).ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	g.Expect(called).To(BeFalse())

	recorder = httptest.NewRecorder()
	request = sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirement-evidence/ownership",
		`{"subjectType":"application","subjectId":"`+uuid.NewString()+
			`","ownershipMarker":"tutorial-fixture","ownershipChecksum":"`+
			canonicalChecksum("a")+`","sourceReference":"evidence://ownership/tutorial",`+
			`"sourceChecksum":"`+canonicalChecksum("b")+`"}`,
		testChannelAuth(),
	)
	registerSampleRetirementOwnershipEvidenceHandler(service).ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusConflict))
	g.Expect(recorder.Body.String()).To(Equal("sample retirement conflicts with current state\n"))
}

func TestSampleRetirementPreviewRequestInjectsAuthenticatedScope(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	organizationID := uuid.New()
	actorID := uuid.New()
	request := validSampleRetirementPreviewRequest()

	domain := request.ToDomain(organizationID, actorID)

	g.Expect(domain.OrganizationID).To(Equal(organizationID))
	g.Expect(domain.RequestedByUserAccountID).To(Equal(actorID))
	g.Expect(domain.BackupReference).To(Equal("s3://immutable-backups/sample-job"))
	g.Expect(domain.RestoreProofReference).To(Equal("evidence://restore/sample-job"))
	g.Expect(domain.Items).To(Equal([]types.SampleRetirementSubject{{
		SubjectType:       types.SampleRetirementSubjectApplication,
		SubjectID:         request.Items[0].SubjectID,
		OwnershipMarker:   "tutorial-fixture",
		OwnershipChecksum: canonicalChecksum("b"),
		ExpectedChecksum:  canonicalChecksum("c"),
	}}))
}

func TestSampleRetirementApplyRequestRequiresExactApprovalContext(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	g.Expect((api.ApplySampleRetirementRequest{
		PreviewChecksum:  canonicalChecksum("a"),
		ApprovalID:       sampleRetirementApprovalID(),
		ApprovalChecksum: canonicalChecksum("f"),
	}).Validate()).To(Succeed())
	g.Expect((api.ApplySampleRetirementRequest{}).Validate()).NotTo(Succeed())
	g.Expect((api.ApplySampleRetirementRequest{
		PreviewChecksum:  "sha256:*",
		ApprovalID:       sampleRetirementApprovalID(),
		ApprovalChecksum: canonicalChecksum("f"),
	}).Validate()).NotTo(Succeed())
	g.Expect((api.ApplySampleRetirementRequest{
		PreviewChecksum:  canonicalChecksum("a"),
		ApprovalID:       "",
		ApprovalChecksum: canonicalChecksum("f"),
	}).Validate()).NotTo(Succeed())
	g.Expect((api.ApplySampleRetirementRequest{
		PreviewChecksum:  canonicalChecksum("a"),
		ApprovalID:       sampleRetirementApprovalID(),
		ApprovalChecksum: "sha256:*",
	}).Validate()).NotTo(Succeed())
}

func TestPreviewSampleRetirementHandlerUsesAuthenticatedOrganization(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	authInfo := testChannelAuth()
	var got types.SampleRetirementRequest
	want := sampleRetirementPreviewFixture(authInfo.orgID)
	service := sampleRetirementServiceStub{
		preview: func(_ context.Context, request types.SampleRetirementRequest) (*types.SampleRetirementPreview, error) {
			got = request
			return &want, nil
		},
	}
	recorder := httptest.NewRecorder()
	request := sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirements/preview",
		validSampleRetirementPreviewJSON(),
		authInfo,
	)

	previewSampleRetirementHandler(service).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusCreated))
	g.Expect(recorder.Header().Get("Content-Type")).To(Equal("application/json"))
	g.Expect(got.OrganizationID).To(Equal(authInfo.orgID))
	g.Expect(got.RequestedByUserAccountID).To(Equal(authInfo.userID))
	g.Expect(recorder.Body.String()).To(ContainSubstring(want.Job.ID.String()))
	g.Expect(recorder.Body.String()).To(ContainSubstring(want.PreviewChecksum))
}

func TestSampleRetirementIDHandlersAreOrganizationScoped(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	authInfo := testChannelAuth()
	jobID := uuid.New()
	checksum := canonicalChecksum("d")
	var getOrgID, applyOrgID, verifyOrgID uuid.UUID
	var applyActorID uuid.UUID
	detail := types.SampleRetirementDetail{
		Job: types.SampleRetirementJob{
			ID: jobID, OrganizationID: authInfo.orgID, PreviewChecksum: checksum,
		},
	}
	result := types.SampleRetirementResult{JobID: jobID, PreviewChecksum: checksum}
	verification := types.SampleRetirementVerification{JobID: jobID, PreviewChecksum: checksum}
	service := sampleRetirementServiceStub{
		get: func(_ context.Context, organizationID, gotJobID uuid.UUID) (*types.SampleRetirementDetail, error) {
			getOrgID = organizationID
			g.Expect(gotJobID).To(Equal(jobID))
			return &detail, nil
		},
		apply: func(
			_ context.Context,
			applyRequest types.SampleRetirementApplyRequest,
		) (*types.SampleRetirementResult, error) {
			applyOrgID = applyRequest.OrganizationID
			applyActorID = applyRequest.ActorUserAccountID
			g.Expect(applyRequest.JobID).To(Equal(jobID))
			g.Expect(applyRequest.PreviewChecksum).To(Equal(checksum))
			g.Expect(applyRequest.ApprovalID).To(Equal(sampleRetirementApprovalID()))
			g.Expect(applyRequest.ApprovalChecksum).To(Equal(canonicalChecksum("f")))
			return &result, nil
		},
		verify: func(
			_ context.Context,
			organizationID, gotJobID uuid.UUID,
		) (*types.SampleRetirementVerification, error) {
			verifyOrgID = organizationID
			g.Expect(gotJobID).To(Equal(jobID))
			return &verification, nil
		},
	}

	tests := []struct {
		name    string
		handler http.Handler
		method  string
		body    string
		want    string
	}{
		{
			name: "get", handler: getSampleRetirementHandler(service),
			method: http.MethodGet, want: `"previewChecksum":"` + checksum + `"`,
		},
		{
			name: "apply", handler: applySampleRetirementHandler(service),
			method: http.MethodPost, body: `{"previewChecksum":"` + checksum +
				`","approvalId":"` + sampleRetirementApprovalID() +
				`","approvalChecksum":"` + canonicalChecksum("f") + `"}`,
			want: `"jobId":"` + jobID.String() + `"`,
		},
		{
			name: "verify", handler: verifySampleRetirementHandler(service),
			method: http.MethodPost, want: `"jobId":"` + jobID.String() + `"`,
		},
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		request := sampleRetirementRequest(
			test.method,
			"/api/v1/sample-retirements/"+jobID.String(),
			test.body,
			authInfo,
		)
		request.SetPathValue("sampleRetirementId", jobID.String())

		test.handler.ServeHTTP(recorder, request)

		g.Expect(recorder.Code).To(Equal(http.StatusOK), test.name)
		g.Expect(recorder.Body.String()).To(ContainSubstring(test.want), test.name)
	}

	g.Expect(getOrgID).To(Equal(authInfo.orgID))
	g.Expect(applyOrgID).To(Equal(authInfo.orgID))
	g.Expect(applyActorID).To(Equal(authInfo.userID))
	g.Expect(verifyOrgID).To(Equal(authInfo.orgID))
}

func TestRequestSampleRetirementApprovalHandlerFreezesAuthenticatedJobContext(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	authInfo := testChannelAuth()
	jobID := uuid.New()
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	var got types.SampleRetirementApprovalRequestInput
	created := types.ApprovalRequest{
		ID:             uuid.New(),
		OrganizationID: authInfo.orgID,
		SubjectType:    types.ApprovalSubjectSampleRetirement,
		SubjectID:      jobID,
		ExpiresAt:      expiresAt,
	}
	service := sampleRetirementServiceStub{
		requestApproval: func(
			_ context.Context,
			input types.SampleRetirementApprovalRequestInput,
		) (*types.ApprovalRequest, error) {
			got = input
			return &created, nil
		},
	}
	recorder := httptest.NewRecorder()
	request := sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirements/"+jobID.String()+"/approval-requests",
		`{"expiresAt":"`+expiresAt.Format(time.RFC3339)+`"}`,
		authInfo,
	)
	request.SetPathValue("sampleRetirementId", jobID.String())

	requestSampleRetirementApprovalHandler(service).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusCreated))
	g.Expect(got.OrganizationID).To(Equal(authInfo.orgID))
	g.Expect(got.SampleRetirementJobID).To(Equal(jobID))
	g.Expect(got.RequestedByUserAccountID).To(Equal(authInfo.userID))
	g.Expect(got.ExpiresAt).To(Equal(expiresAt))
	g.Expect(got.Authorize).NotTo(BeNil())
	g.Expect(recorder.Body.String()).To(ContainSubstring(created.ID.String()))
	g.Expect(recorder.Body.String()).To(ContainSubstring(`"subjectType":"sample_retirement"`))
}

func TestSampleRetirementApprovalAuthorizationBindsExactJobAndAction(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	authInfo := testChannelAuth()
	jobID := uuid.New()
	evidence := types.ApprovalAuthorizationContext{
		OrganizationID:        authInfo.orgID,
		ActorUserAccountID:    authInfo.userID,
		DecisionAt:            time.Now().UTC(),
		SampleRetirementJobID: jobID,
	}
	var gotResource types.ResourceRef
	var gotRequest types.AccessRequest
	dependencies := controlPlaneResourceAuthorizationDependencies{
		resolveScopes: func(
			_ context.Context,
			resource types.ResourceRef,
		) ([]types.ScopeRef, error) {
			gotResource = resource
			return []types.ScopeRef{{
				Kind: types.PermissionScopeOrganization,
				ID:   authInfo.orgID,
			}}, nil
		},
		authorize: func(
			_ context.Context,
			request types.AccessRequest,
		) (types.AccessDecision, error) {
			gotRequest = request
			return types.AccessDecision{Allowed: true}, nil
		},
	}

	err := authorizeSampleRetirementApprovalWithDependencies(
		t.Context(),
		authInfo,
		jobID,
		evidence,
		dependencies,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotResource).To(Equal(types.ResourceRef{
		OrganizationID: authInfo.orgID,
		Kind:           types.PermissionScopeOrganization,
		ID:             authInfo.orgID,
	}))
	g.Expect(gotRequest.Action).To(Equal(types.ActionSampleRetire))
	g.Expect(gotRequest.DecisionAt).To(Equal(evidence.DecisionAt))

	evidence.SampleRetirementJobID = uuid.New()
	err = authorizeSampleRetirementApprovalWithDependencies(
		t.Context(),
		authInfo,
		jobID,
		evidence,
		dependencies,
	)
	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
}

func TestSampleRetirementApplyResolvesApprovedServerEvidence(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	organizationID := uuid.New()
	jobID := uuid.New()
	approvalID := uuid.MustParse(sampleRetirementApprovalID())
	request := types.SampleRetirementApplyRequest{
		OrganizationID:     organizationID,
		ActorUserAccountID: uuid.New(),
		JobID:              jobID,
		PreviewChecksum:    canonicalChecksum("a"),
		ApprovalID:         approvalID.String(),
		ApprovalChecksum:   canonicalChecksum("b"),
	}
	resolverCalled := false
	resolver := func(
		_ context.Context,
		gotOrganizationID, gotJobID, gotApprovalID uuid.UUID,
	) (types.SampleRetirementApprovalBinding, error) {
		resolverCalled = true
		g.Expect(gotOrganizationID).To(Equal(organizationID))
		g.Expect(gotJobID).To(Equal(jobID))
		g.Expect(gotApprovalID).To(Equal(approvalID))
		return types.SampleRetirementApprovalBinding{
			ApprovalRequestID: approvalID,
			ApprovalChecksum:  canonicalChecksum("b"),
		}, nil
	}

	resolved, err := resolveSampleRetirementApplyApproval(
		t.Context(),
		request,
		resolver,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resolverCalled).To(BeTrue())
	g.Expect(resolved).To(Equal(request))

	stale := request
	stale.ApprovalChecksum = canonicalChecksum("c")
	_, err = resolveSampleRetirementApplyApproval(t.Context(), stale, resolver)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestSampleRetirementHandlersRejectMalformedIDsAndBodiesBeforeServiceAccess(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	called := false
	service := sampleRetirementServiceStub{
		preview: func(context.Context, types.SampleRetirementRequest) (*types.SampleRetirementPreview, error) {
			called = true
			preview := types.SampleRetirementPreview{}
			return &preview, nil
		},
		get: func(context.Context, uuid.UUID, uuid.UUID) (*types.SampleRetirementDetail, error) {
			called = true
			return nil, nil
		},
		apply: func(context.Context, types.SampleRetirementApplyRequest) (*types.SampleRetirementResult, error) {
			called = true
			return nil, nil
		},
	}
	authInfo := testChannelAuth()

	recorder := httptest.NewRecorder()
	request := sampleRetirementRequest(http.MethodGet, "/api/v1/sample-retirements/not-a-uuid", "", authInfo)
	request.SetPathValue("sampleRetirementId", "not-a-uuid")
	getSampleRetirementHandler(service).ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))

	recorder = httptest.NewRecorder()
	request = sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirements/"+uuid.NewString()+"/apply",
		`{"previewChecksum":"sha256:*"}`,
		authInfo,
	)
	request.SetPathValue("sampleRetirementId", uuid.NewString())
	applySampleRetirementHandler(service).ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))

	jobID := uuid.NewString()
	recorder = httptest.NewRecorder()
	request = sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirements/"+jobID+"/apply",
		`{"previewChecksum":"`+canonicalChecksum("a")+
			`","approvalId":"approval id","approvalChecksum":"`+canonicalChecksum("b")+`"}`,
		authInfo,
	)
	request.SetPathValue("sampleRetirementId", jobID)
	applySampleRetirementHandler(service).ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))

	recorder = httptest.NewRecorder()
	request = sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirements/preview",
		strings.Replace(validSampleRetirementPreviewJSON(), "{", `{"wildcard":"*",`, 1),
		authInfo,
	)
	previewSampleRetirementHandler(service).ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	g.Expect(called).To(BeFalse())
}

func TestSampleRetirementErrorsUseStableSanitizedContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{
			name: "bad request", err: apierrors.ErrBadRequest,
			wantStatus: http.StatusBadRequest, wantBody: "sample retirement request is invalid\n",
		},
		{
			name: "forbidden", err: apierrors.ErrForbidden,
			wantStatus: http.StatusForbidden, wantBody: "sample retirement operation is forbidden\n",
		},
		{
			name: "not found", err: apierrors.ErrNotFound,
			wantStatus: http.StatusNotFound, wantBody: "sample retirement not found\n",
		},
		{
			name: "conflict", err: apierrors.ErrConflict,
			wantStatus: http.StatusConflict, wantBody: "sample retirement conflicts with current state\n",
		},
		{
			name: "internal error", err: errors.New("database secret [REDACTED_SECRET]"),
			wantStatus: http.StatusInternalServerError, wantBody: "sample retirement operation failed\n",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(internalctx.WithLogger(request.Context(), zap.NewNop()))

			handleSampleRetirementError(recorder, request, "test", test.err)

			g.Expect(recorder.Code).To(Equal(test.wantStatus))
			g.Expect(recorder.Body.String()).To(Equal(test.wantBody))
			g.Expect(recorder.Body.String()).NotTo(ContainSubstring("[REDACTED_SECRET]"))
		})
	}
}

func TestSampleRetirementHandlersFailClosedOnEmptyServiceResults(t *testing.T) {
	t.Parallel()

	service := sampleRetirementServiceStub{
		preview: func(
			context.Context,
			types.SampleRetirementRequest,
		) (*types.SampleRetirementPreview, error) {
			return nil, nil
		},
	}
	recorder := httptest.NewRecorder()
	request := sampleRetirementRequest(
		http.MethodPost,
		"/api/v1/sample-retirements/preview",
		validSampleRetirementPreviewJSON(),
		testChannelAuth(),
	)

	previewSampleRetirementHandler(service).ServeHTTP(recorder, request)

	g := NewWithT(t)
	g.Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
	g.Expect(recorder.Body.String()).To(Equal("sample retirement operation failed\n"))
}

type sampleRetirementServiceStub struct {
	preview         func(context.Context, types.SampleRetirementRequest) (*types.SampleRetirementPreview, error)
	get             func(context.Context, uuid.UUID, uuid.UUID) (*types.SampleRetirementDetail, error)
	requestApproval func(context.Context, types.SampleRetirementApprovalRequestInput) (*types.ApprovalRequest, error)
	apply           func(context.Context, types.SampleRetirementApplyRequest) (*types.SampleRetirementResult, error)
	verify          func(context.Context, uuid.UUID, uuid.UUID) (*types.SampleRetirementVerification, error)
}

type sampleRetirementEvidenceServiceStub struct {
	registerOwnership func(
		context.Context,
		types.SampleRetirementOwnershipEvidenceRegistrationInput,
	) (*types.SampleRetirementOwnershipEvidence, error)
	registerRecovery func(
		context.Context,
		types.SampleRetirementRecoveryEvidenceRegistrationInput,
	) (*types.SampleRetirementRecoveryEvidence, error)
}

func (s sampleRetirementEvidenceServiceStub) RegisterSampleRetirementOwnershipEvidence(
	ctx context.Context,
	input types.SampleRetirementOwnershipEvidenceRegistrationInput,
) (*types.SampleRetirementOwnershipEvidence, error) {
	return s.registerOwnership(ctx, input)
}

func (s sampleRetirementEvidenceServiceStub) RegisterSampleRetirementRecoveryEvidence(
	ctx context.Context,
	input types.SampleRetirementRecoveryEvidenceRegistrationInput,
) (*types.SampleRetirementRecoveryEvidence, error) {
	return s.registerRecovery(ctx, input)
}

func (s sampleRetirementServiceStub) PreviewSampleRetirement(
	ctx context.Context,
	request types.SampleRetirementRequest,
) (*types.SampleRetirementPreview, error) {
	return s.preview(ctx, request)
}

func (s sampleRetirementServiceStub) GetSampleRetirement(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (*types.SampleRetirementDetail, error) {
	return s.get(ctx, organizationID, jobID)
}

func (s sampleRetirementServiceStub) RequestSampleRetirementApproval(
	ctx context.Context,
	input types.SampleRetirementApprovalRequestInput,
) (*types.ApprovalRequest, error) {
	return s.requestApproval(ctx, input)
}

func (s sampleRetirementServiceStub) ApplySampleRetirement(
	ctx context.Context,
	request types.SampleRetirementApplyRequest,
) (*types.SampleRetirementResult, error) {
	return s.apply(ctx, request)
}

func (s sampleRetirementServiceStub) VerifySampleRetirement(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (*types.SampleRetirementVerification, error) {
	return s.verify(ctx, organizationID, jobID)
}

func validSampleRetirementPreviewRequest() api.SampleRetirementPreviewRequest {
	return api.SampleRetirementPreviewRequest{
		BackupReference:       "s3://immutable-backups/sample-job",
		BackupChecksum:        canonicalChecksum("a"),
		RestoreProofReference: "evidence://restore/sample-job",
		RestoreProofChecksum:  canonicalChecksum("d"),
		Items: []api.SampleRetirementSubject{{
			SubjectType:       types.SampleRetirementSubjectApplication,
			SubjectID:         uuid.MustParse("11111111-1111-4111-8111-111111111111"),
			OwnershipMarker:   "tutorial-fixture",
			OwnershipChecksum: canonicalChecksum("b"),
			ExpectedChecksum:  canonicalChecksum("c"),
		}},
	}
}

func validSampleRetirementPreviewJSON() string {
	return `{
		"backupReference":"s3://immutable-backups/sample-job",
		"backupChecksum":"` + canonicalChecksum("a") + `",
		"restoreProofReference":"evidence://restore/sample-job",
		"restoreProofChecksum":"` + canonicalChecksum("d") + `",
		"items":[{
			"subjectType":"application",
			"subjectId":"11111111-1111-4111-8111-111111111111",
			"ownershipMarker":"tutorial-fixture",
			"ownershipChecksum":"` + canonicalChecksum("b") + `",
			"expectedChecksum":"` + canonicalChecksum("c") + `"
		}]
	}`
}

func canonicalChecksum(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func sampleRetirementApprovalID() string {
	return "33333333-3333-4333-8333-333333333333"
}

func sampleRetirementPreviewFixture(organizationID uuid.UUID) types.SampleRetirementPreview {
	createdAt := time.Date(2026, time.July, 28, 1, 2, 3, 0, time.UTC)
	return types.SampleRetirementPreview{
		Job: types.SampleRetirementJob{
			ID:             uuid.MustParse("22222222-2222-4222-8222-222222222222"),
			OrganizationID: organizationID,
			State:          types.SampleRetirementJobPreviewed,
		},
		PreviewChecksum: canonicalChecksum("e"),
		RequestedCount:  1,
		RetirableCount:  1,
		CreatedAt:       createdAt,
	}
}

func sampleRetirementRequest(
	method, target, body string,
	authInfo channelTestAuth,
) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	return request.WithContext(auth.Authentication.NewContext(ctx, authInfo))
}

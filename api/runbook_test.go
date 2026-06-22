package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateUpdateRunbookRequestValidate(t *testing.T) {
	g := NewWithT(t)
	request := CreateUpdateRunbookRequest{
		ApplicationID: uuid.New(),
		Name:          " Rotate keys ",
		Description:   "description",
		SortOrder:     10,
	}

	g.Expect(request.Validate()).To(Succeed())

	g.Expect(request.Name).To(Equal("Rotate keys"))
}

func TestCreateUpdateRunbookRequestValidateRejectsInvalidPayloads(t *testing.T) {
	applicationID := uuid.New()

	tests := []struct {
		name    string
		request CreateUpdateRunbookRequest
		want    string
	}{
		{
			name:    "empty name",
			request: CreateUpdateRunbookRequest{ApplicationID: applicationID, Name: " "},
			want:    "name is required",
		},
		{
			name:    "missing application id",
			request: CreateUpdateRunbookRequest{Name: "Rotate keys"},
			want:    "applicationId is required",
		},
		{
			name:    "negative sort order",
			request: CreateUpdateRunbookRequest{ApplicationID: applicationID, Name: "Rotate keys", SortOrder: -1},
			want:    "sortOrder must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tt.want))
		})
	}
}

func TestCreateRunbookRevisionRequestValidateNormalizesSteps(t *testing.T) {
	g := NewWithT(t)
	request := CreateRunbookRevisionRequest{
		Description: " initial revision ",
		Steps: []RunbookStepRequest{
			{
				Key:                 " verify ",
				Name:                " Verify ",
				ActionType:          " distr.preflight ",
				ExecutionLocation:   " hub ",
				InputBindings:       map[string]any{},
				Condition:           " always() ",
				FailureMode:         " ",
				TimeoutSeconds:      120,
				RetryPolicy:         RunbookStepRetryPolicyRequest{MaxAttempts: 3, IntervalSeconds: 10},
				RequiredPermissions: []string{" runbook:execute "},
				SortOrder:           20,
				Dependencies:        []string{" prepare "},
			},
			{
				Key:               " prepare ",
				Name:              " Prepare ",
				ActionType:        " distr.preflight ",
				ExecutionLocation: " hub ",
				SortOrder:         10,
			},
		},
	}

	g.Expect(request.Validate()).To(Succeed())

	g.Expect(request.Description).To(Equal("initial revision"))
	g.Expect(request.Steps[0].Key).To(Equal("verify"))
	g.Expect(request.Steps[0].Name).To(Equal("Verify"))
	g.Expect(request.Steps[0].ActionType).To(Equal("distr.preflight"))
	g.Expect(request.Steps[0].ExecutionLocation).To(Equal("hub"))
	g.Expect(request.Steps[0].Condition).To(Equal("always()"))
	g.Expect(request.Steps[0].FailureMode).To(Equal("fail"))
	g.Expect(request.Steps[0].RequiredPermissions).To(Equal([]string{"runbook:execute"}))
	g.Expect(request.Steps[0].Dependencies).To(Equal([]string{"prepare"}))
	g.Expect(request.Steps[1].InputBindings).To(Equal(map[string]any{}))
}

func TestCreateRunbookRevisionRequestValidateRejectsInvalidSteps(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*CreateRunbookRevisionRequest)
		wantErr string
	}{
		{
			name:    "no steps",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps = nil },
			wantErr: "at least one step is required",
		},
		{
			name:    "empty step key",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].Key = " " },
			wantErr: "step key is required",
		},
		{
			name:    "empty step name",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].Name = " " },
			wantErr: "step name is required",
		},
		{
			name:    "empty action type",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].ActionType = " " },
			wantErr: "step actionType is required",
		},
		{
			name:    "unknown action type",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].ActionType = "script" },
			wantErr: `unknown actionType "script"`,
		},
		{
			name:    "empty execution location",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].ExecutionLocation = " " },
			wantErr: "step executionLocation is required",
		},
		{
			name: "duplicate trimmed keys",
			mutate: func(r *CreateRunbookRevisionRequest) {
				r.Steps = append(r.Steps, validRunbookStepRequest(" verify ", 30))
			},
			wantErr: "step keys must be unique",
		},
		{
			name: "duplicate sort orders",
			mutate: func(r *CreateRunbookRevisionRequest) {
				r.Steps = append(r.Steps, validRunbookStepRequest("deploy", 20))
			},
			wantErr: "step sortOrder values must be unique",
		},
		{
			name:    "negative sort order",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].SortOrder = -1 },
			wantErr: "step sortOrder must be non-negative",
		},
		{
			name:    "negative timeout",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].TimeoutSeconds = -1 },
			wantErr: "step timeoutSeconds must be non-negative",
		},
		{
			name:    "negative retry max attempts",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].RetryPolicy.MaxAttempts = -1 },
			wantErr: "step retryPolicy.maxAttempts must be non-negative",
		},
		{
			name:    "negative retry interval",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].RetryPolicy.IntervalSeconds = -1 },
			wantErr: "step retryPolicy.intervalSeconds must be non-negative",
		},
		{
			name:    "empty required permission",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].RequiredPermissions = []string{" "} },
			wantErr: "step requiredPermissions must not contain empty values",
		},
		{
			name:    "missing dependency",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].Dependencies = []string{"missing"} },
			wantErr: `step dependency "missing" does not exist`,
		},
		{
			name:    "invalid condition",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].Condition = `channel =~ "Stable"` },
			wantErr: "condition is invalid",
		},
		{
			name: "condition output reference missing step",
			mutate: func(r *CreateRunbookRevisionRequest) {
				r.Steps[0].Condition = `output("missing", "status") == "ok"`
			},
			wantErr: `step condition output reference "missing" does not exist`,
		},
		{
			name: "condition output reference cycle",
			mutate: func(r *CreateRunbookRevisionRequest) {
				r.Steps[0].Condition = `output("prepare", "status") == "ok"`
				r.Steps[1].Condition = `output("verify", "status") == "ok"`
			},
			wantErr: "step dependencies must not contain cycles",
		},
		{
			name:    "self dependency",
			mutate:  func(r *CreateRunbookRevisionRequest) { r.Steps[0].Dependencies = []string{"verify"} },
			wantErr: "step cannot depend on itself",
		},
		{
			name: "duplicate dependency",
			mutate: func(r *CreateRunbookRevisionRequest) {
				r.Steps[0].Dependencies = []string{"prepare", " prepare "}
			},
			wantErr: "step dependencies must be unique",
		},
		{
			name: "cycle",
			mutate: func(r *CreateRunbookRevisionRequest) {
				r.Steps[0].Dependencies = []string{"prepare"}
				r.Steps[1].Dependencies = []string{"verify"}
			},
			wantErr: "step dependencies must not contain cycles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := validRunbookRevisionRequest()
			tt.mutate(&request)

			err := request.Validate()

			g.Expect(err).To(HaveOccurred())
			g.Expect(strings.ToLower(err.Error())).To(ContainSubstring(strings.ToLower(tt.wantErr)))
		})
	}
}

func validRunbookRevisionRequest() CreateRunbookRevisionRequest {
	return CreateRunbookRevisionRequest{
		Steps: []RunbookStepRequest{
			validRunbookStepRequest("verify", 20),
			validRunbookStepRequest("prepare", 10),
		},
	}
}

func validRunbookStepRequest(key string, sortOrder int) RunbookStepRequest {
	return RunbookStepRequest{
		Key:               key,
		Name:              key,
		ActionType:        "distr.preflight",
		InputBindings:     map[string]any{},
		ExecutionLocation: "hub",
		SortOrder:         sortOrder,
	}
}

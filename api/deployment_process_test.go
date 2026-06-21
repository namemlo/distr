package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateUpdateDeploymentProcessRequestValidate(t *testing.T) {
	g := NewWithT(t)
	request := CreateUpdateDeploymentProcessRequest{
		ApplicationID: uuid.New(),
		Name:          " Standard deploy ",
		Description:   "description",
		SortOrder:     10,
	}

	g.Expect(request.Validate()).To(Succeed())

	g.Expect(request.Name).To(Equal("Standard deploy"))
}

func TestCreateUpdateDeploymentProcessRequestValidateRejectsInvalidPayloads(t *testing.T) {
	applicationID := uuid.New()

	tests := []struct {
		name    string
		request CreateUpdateDeploymentProcessRequest
		want    string
	}{
		{
			name:    "empty name",
			request: CreateUpdateDeploymentProcessRequest{ApplicationID: applicationID, Name: " "},
			want:    "name is required",
		},
		{
			name:    "missing application id",
			request: CreateUpdateDeploymentProcessRequest{Name: "Standard deploy"},
			want:    "applicationId is required",
		},
		{
			name:    "negative sort order",
			request: CreateUpdateDeploymentProcessRequest{ApplicationID: applicationID, Name: "Standard deploy", SortOrder: -1},
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

func TestCreateDeploymentProcessRevisionRequestValidateNormalizesSteps(t *testing.T) {
	g := NewWithT(t)
	channelID := uuid.New()
	environmentID := uuid.New()
	request := CreateDeploymentProcessRevisionRequest{
		Description: " initial revision ",
		Steps: []DeploymentProcessStepRequest{
			{
				Key:                 " build ",
				Name:                " Build ",
				ActionType:          " distr.http.check ",
				ExecutionLocation:   " hub ",
				InputBindings:       map[string]any{"url": "https://example.com/health"},
				Condition:           " channel == stable ",
				ChannelIDs:          []uuid.UUID{channelID},
				EnvironmentIDs:      []uuid.UUID{environmentID},
				TargetTags:          []string{" linux ", " x64 "},
				FailureMode:         " fail ",
				TimeoutSeconds:      120,
				RetryPolicy:         DeploymentProcessStepRetryPolicyRequest{MaxAttempts: 3, IntervalSeconds: 10},
				RequiredPermissions: []string{" deploy:write "},
				SortOrder:           10,
				Dependencies:        []string{" prepare "},
			},
			{
				Key:               " prepare ",
				Name:              " Prepare ",
				ActionType:        " distr.preflight ",
				ExecutionLocation: " hub ",
				InputBindings:     map[string]any{},
				SortOrder:         0,
			},
		},
	}

	g.Expect(request.Validate()).To(Succeed())

	g.Expect(request.Description).To(Equal("initial revision"))
	g.Expect(request.Steps[0].Key).To(Equal("build"))
	g.Expect(request.Steps[0].Name).To(Equal("Build"))
	g.Expect(request.Steps[0].ActionType).To(Equal("distr.http.check"))
	g.Expect(request.Steps[0].ExecutionLocation).To(Equal("hub"))
	g.Expect(request.Steps[0].Condition).To(Equal("channel == stable"))
	g.Expect(request.Steps[0].TargetTags).To(Equal([]string{"linux", "x64"}))
	g.Expect(request.Steps[0].FailureMode).To(Equal("fail"))
	g.Expect(request.Steps[0].RequiredPermissions).To(Equal([]string{"deploy:write"}))
	g.Expect(request.Steps[0].Dependencies).To(Equal([]string{"prepare"}))
}

func TestCreateDeploymentProcessRevisionRequestValidateRejectsInvalidSteps(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*CreateDeploymentProcessRevisionRequest)
		wantErr string
	}{
		{
			name:    "no steps",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps = nil },
			wantErr: "at least one step is required",
		},
		{
			name:    "empty step key",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].Key = " " },
			wantErr: "step key is required",
		},
		{
			name:    "empty step name",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].Name = " " },
			wantErr: "step name is required",
		},
		{
			name:    "empty action type",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].ActionType = " " },
			wantErr: "step actionType is required",
		},
		{
			name:    "unknown action type",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].ActionType = "script" },
			wantErr: `unknown actionType "script"`,
		},
		{
			name: "registered action input does not match schema",
			mutate: func(r *CreateDeploymentProcessRevisionRequest) {
				r.Steps[0].ActionType = "distr.http.check"
				r.Steps[0].InputBindings = map[string]any{"expectedStatusCodes": []any{float64(200)}}
			},
			wantErr: "url",
		},
		{
			name:    "empty execution location",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].ExecutionLocation = " " },
			wantErr: "step executionLocation is required",
		},
		{
			name: "duplicate trimmed keys",
			mutate: func(r *CreateDeploymentProcessRevisionRequest) {
				r.Steps = append(r.Steps, validDeploymentProcessStepRequest(" build ", 20))
			},
			wantErr: "step keys must be unique",
		},
		{
			name: "duplicate sort orders",
			mutate: func(r *CreateDeploymentProcessRevisionRequest) {
				r.Steps = append(r.Steps, validDeploymentProcessStepRequest("deploy", 10))
			},
			wantErr: "step sortOrder values must be unique",
		},
		{
			name:    "negative sort order",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].SortOrder = -1 },
			wantErr: "step sortOrder must be non-negative",
		},
		{
			name:    "negative timeout",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].TimeoutSeconds = -1 },
			wantErr: "step timeoutSeconds must be non-negative",
		},
		{
			name:    "negative retry max attempts",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].RetryPolicy.MaxAttempts = -1 },
			wantErr: "step retryPolicy.maxAttempts must be non-negative",
		},
		{
			name:    "negative retry interval",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].RetryPolicy.IntervalSeconds = -1 },
			wantErr: "step retryPolicy.intervalSeconds must be non-negative",
		},
		{
			name:    "nil channel id",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].ChannelIDs = []uuid.UUID{uuid.Nil} },
			wantErr: "step channelIds must not contain empty IDs",
		},
		{
			name:    "nil environment id",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].EnvironmentIDs = []uuid.UUID{uuid.Nil} },
			wantErr: "step environmentIds must not contain empty IDs",
		},
		{
			name:    "empty target tag",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].TargetTags = []string{" "} },
			wantErr: "step targetTags must not contain empty values",
		},
		{
			name:    "empty required permission",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].RequiredPermissions = []string{" "} },
			wantErr: "step requiredPermissions must not contain empty values",
		},
		{
			name:    "missing dependency",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].Dependencies = []string{"missing"} },
			wantErr: `step dependency "missing" does not exist`,
		},
		{
			name:    "self dependency",
			mutate:  func(r *CreateDeploymentProcessRevisionRequest) { r.Steps[0].Dependencies = []string{"build"} },
			wantErr: "step cannot depend on itself",
		},
		{
			name: "duplicate dependency",
			mutate: func(r *CreateDeploymentProcessRevisionRequest) {
				r.Steps[0].Dependencies = []string{"prepare", " prepare "}
			},
			wantErr: "step dependencies must be unique",
		},
		{
			name: "cycle",
			mutate: func(r *CreateDeploymentProcessRevisionRequest) {
				r.Steps[0].Dependencies = []string{"prepare"}
				r.Steps[1].Dependencies = []string{"build"}
			},
			wantErr: "step dependencies must not contain cycles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := validDeploymentProcessRevisionRequest()
			tt.mutate(&request)

			err := request.Validate()

			g.Expect(err).To(HaveOccurred())
			g.Expect(strings.ToLower(err.Error())).To(ContainSubstring(strings.ToLower(tt.wantErr)))
		})
	}
}

func validDeploymentProcessRevisionRequest() CreateDeploymentProcessRevisionRequest {
	return CreateDeploymentProcessRevisionRequest{
		Steps: []DeploymentProcessStepRequest{
			validDeploymentProcessStepRequest("build", 10),
			validDeploymentProcessStepRequest("prepare", 0),
		},
	}
}

func validDeploymentProcessStepRequest(key string, sortOrder int) DeploymentProcessStepRequest {
	return DeploymentProcessStepRequest{
		Key:               key,
		Name:              key,
		ActionType:        "distr.preflight",
		InputBindings:     map[string]any{},
		ExecutionLocation: "hub",
		SortOrder:         sortOrder,
	}
}

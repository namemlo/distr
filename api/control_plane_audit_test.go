package api

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestControlPlaneAuditListRequestValidatesStablePageBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request ControlPlaneAuditListRequest
		wantErr string
	}{
		{name: "default", request: ControlPlaneAuditListRequest{}},
		{name: "maximum", request: ControlPlaneAuditListRequest{Limit: 100}},
		{name: "negative cursor", request: ControlPlaneAuditListRequest{AfterSequence: -1}, wantErr: "afterSequence"},
		{name: "oversize", request: ControlPlaneAuditListRequest{Limit: 101}, wantErr: "limit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.request.Validate()
			if test.wantErr == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestEvidenceBundleRequestRequiresDeploymentPlan(t *testing.T) {
	t.Parallel()

	if err := (EvidenceBundleRequest{}).Validate(); err == nil {
		t.Fatal("Validate() accepted empty deploymentPlanId")
	}
	if err := (EvidenceBundleRequest{DeploymentPlanID: uuid.New()}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCreateAuditExportSinkRequestValidatesReferencesWithoutSecretMaterial(t *testing.T) {
	g := NewWithT(t)
	valid := CreateAuditExportSinkRequest{
		Name:              " Security archive ",
		Kind:              types.AuditExportSinkKindSIEM,
		EndpointReference: "secret://audit/siem-endpoint",
		ConfigChecksum:    "sha256:" + strings.Repeat("a", 64),
	}
	g.Expect(valid.Validate()).To(Succeed())

	for name, mutate := range map[string]func(*CreateAuditExportSinkRequest){
		"empty name": func(request *CreateAuditExportSinkRequest) {
			request.Name = " "
		},
		"unsupported kind": func(request *CreateAuditExportSinkRequest) {
			request.Kind = "shell"
		},
		"inline credential": func(request *CreateAuditExportSinkRequest) {
			request.EndpointReference = "https://user:password@example.test/export"
		},
		"raw network endpoint": func(request *CreateAuditExportSinkRequest) {
			request.EndpointReference = "https://siem.example.test/export"
		},
		"reference traversal": func(request *CreateAuditExportSinkRequest) {
			request.EndpointReference = "secret://audit/../admin"
		},
		"reference query": func(request *CreateAuditExportSinkRequest) {
			request.EndpointReference = "secret://audit/siem?token=inline"
		},
		"invalid checksum": func(request *CreateAuditExportSinkRequest) {
			request.ConfigChecksum = "sha256:not-a-digest"
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			request := valid
			mutate(&request)
			g.Expect(request.Validate()).To(HaveOccurred())
		})
	}
}

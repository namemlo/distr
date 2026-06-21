package api

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestHeartbeatAgentTaskLeaseRequestValidate(t *testing.T) {
	tests := []struct {
		name      string
		request   HeartbeatAgentTaskLeaseRequest
		wantError string
	}{
		{
			name:    "valid",
			request: HeartbeatAgentTaskLeaseRequest{LeaseToken: " lease-token "},
		},
		{
			name:      "missing token",
			request:   HeartbeatAgentTaskLeaseRequest{},
			wantError: "leaseToken is required",
		},
		{
			name:      "blank token",
			request:   HeartbeatAgentTaskLeaseRequest{LeaseToken: " \t "},
			wantError: "leaseToken is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantError == "" {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tt.request.LeaseToken).To(Equal("lease-token"))
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantError)))
			}
		})
	}
}

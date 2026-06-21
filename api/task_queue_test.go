package api

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestTransitionTaskStateRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request TransitionTaskStateRequest
		wantErr string
	}{
		{
			name:    "accepts running status",
			request: TransitionTaskStateRequest{Status: types.TaskStatusRunning},
		},
		{
			name:    "missing status",
			request: TransitionTaskStateRequest{},
			wantErr: "status is required",
		},
		{
			name:    "invalid status",
			request: TransitionTaskStateRequest{Status: types.TaskStatus("PAUSED")},
			wantErr: "status is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			}
		})
	}
}

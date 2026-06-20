package api

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateUpdateChannelRequestValidate(t *testing.T) {
	applicationID := uuid.New()
	lifecycleID := uuid.New()

	tests := []struct {
		name    string
		request CreateUpdateChannelRequest
		wantErr bool
	}{
		{
			name: "accepts complete channel settings",
			request: CreateUpdateChannelRequest{
				ApplicationID: applicationID,
				LifecycleID:   lifecycleID,
				Name:          "Stable",
				Description:   "Default production-ready channel",
				SortOrder:     10,
				IsDefault:     true,
			},
			wantErr: false,
		},
		{
			name: "rejects blank names",
			request: CreateUpdateChannelRequest{
				ApplicationID: applicationID,
				LifecycleID:   lifecycleID,
				Name:          "   ",
			},
			wantErr: true,
		},
		{
			name: "rejects missing application references",
			request: CreateUpdateChannelRequest{
				LifecycleID: lifecycleID,
				Name:        "Stable",
			},
			wantErr: true,
		},
		{
			name: "rejects missing lifecycle references",
			request: CreateUpdateChannelRequest{
				ApplicationID: applicationID,
				Name:          "Stable",
			},
			wantErr: true,
		},
		{
			name: "rejects negative sort order",
			request: CreateUpdateChannelRequest{
				ApplicationID: applicationID,
				LifecycleID:   lifecycleID,
				Name:          "Stable",
				SortOrder:     -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

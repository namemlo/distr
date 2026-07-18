package handlers

import (
	"net/http"
	"os"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	. "github.com/onsi/gomega"
)

func TestObservationCredentialParsingRejectsWrongSchemeAndShortSecret(t *testing.T) {
	g := NewWithT(t)
	request, err := http.NewRequest(http.MethodPost, "/", nil)
	g.Expect(err).NotTo(HaveOccurred())
	request.Header.Set("Authorization", "Bearer observer-secret-at-least-32-characters")
	_, err = observerCredential(request)
	g.Expect(err).To(HaveOccurred())

	request.Header.Set("Authorization", "Observer short")
	_, err = observerCredential(request)
	g.Expect(err).To(HaveOccurred())

	request.Header.Set("Authorization", "Observer observer-secret-at-least-32-characters")
	credential, err := observerCredential(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(credential).To(Equal("observer-secret-at-least-32-characters"))
}

func TestObservationRoutesSeparateObserverAuthFromManagementAuth(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("observations.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring("ObserverIngestRouter"))
	g.Expect(text).To(ContainSubstring("ObserverRegistrationsRouter"))
	g.Expect(text).To(ContainSubstring("ObservationsRouter"))
	g.Expect(text).To(ContainSubstring("middleware.RequireOrgAndRole"))
	g.Expect(text).To(ContainSubstring("featureflags.KeyOperatorControlPlaneV2"))
	g.Expect(text).To(ContainSubstring("middleware.RequireReadWriteOrAdmin"))
	g.Expect(text).To(ContainSubstring("api.ObserverCredentialFingerprint"))
}

func TestObservationPublicErrorDoesNotLeakTrustOrTenantDetails(t *testing.T) {
	g := NewWithT(t)
	status, message := observationPublicError(
		apierrors.NewConflict("foreign tenant observer credential fingerprint"),
	)
	g.Expect(status).To(Equal(http.StatusConflict))
	g.Expect(message).To(Equal("observation conflicts with retained evidence"))
	g.Expect(message).NotTo(ContainSubstring("credential"))
}

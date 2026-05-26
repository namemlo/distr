package types

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestDeploymentStatusTypeParsing(t *testing.T) {
	g := NewWithT(t)

	var target struct {
		Type DeploymentStatusType `json:"type"`
	}

	err := json.Unmarshal([]byte(`{"type": "healthy"}`), &target)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.Type).To(Equal(DeploymentStatusTypeHealthy))

	err = json.Unmarshal([]byte(`{"type": "ok"}`), &target)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.Type).To(Equal(DeploymentStatusTypeRunning))

	err = json.Unmarshal([]byte(`{"type": "does-not-exist"}`), &target)
	g.Expect(err).To(MatchError(ErrInvalidDeploymentStatusType))
}

func TestAccountRoleParsing(t *testing.T) {
	g := NewWithT(t)

	var target struct {
		Role AccountRole `json:"role"`
	}

	err := json.Unmarshal([]byte(`{"role": "read_only"}`), &target)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.Role).To(Equal(AccountRoleReadOnly))

	err = json.Unmarshal([]byte(`{"role": "read_write"}`), &target)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.Role).To(Equal(AccountRoleReadWrite))

	err = json.Unmarshal([]byte(`{"role": "admin"}`), &target)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.Role).To(Equal(AccountRoleAdmin))

	err = json.Unmarshal([]byte(`{"role": "superuser"}`), &target)
	g.Expect(err).To(HaveOccurred())
}

func TestDeploymentTypeParsing(t *testing.T) {
	g := NewWithT(t)

	var target struct {
		Type DeploymentType `json:"type"`
	}

	err := json.Unmarshal([]byte(`{"type": "docker"}`), &target)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.Type).To(Equal(DeploymentTypeDocker))

	err = json.Unmarshal([]byte(`{"type": "kubernetes"}`), &target)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(target.Type).To(Equal(DeploymentTypeKubernetes))

	err = json.Unmarshal([]byte(`{"type": "swarm"}`), &target)
	g.Expect(err).To(MatchError(ErrInvalidDeploymentType))
}

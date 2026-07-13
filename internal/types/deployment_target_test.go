package types

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestDeploymentTargetValidateNormalizesAndValidatesPlatform(t *testing.T) {
	g := NewWithT(t)

	defaultTarget := DeploymentTarget{Type: DeploymentTypeDocker}
	g.Expect(defaultTarget.Validate()).To(Succeed())
	g.Expect(defaultTarget.Platform).To(Equal(DeploymentTargetPlatformLinuxAMD64))

	armTarget := DeploymentTarget{Type: DeploymentTypeDocker, Platform: DeploymentTargetPlatformLinuxARM64}
	g.Expect(armTarget.Validate()).To(Succeed())

	invalidTarget := DeploymentTarget{Type: DeploymentTypeDocker, Platform: "linux/386"}
	g.Expect(invalidTarget.Validate()).To(MatchError(ContainSubstring("platform")))
}

package types

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestTaskFreezesProtocolVersion(t *testing.T) {
	g := NewWithT(t)
	task := Task{ProtocolVersion: ExecutionProtocolVersionV2}
	payload, err := json.Marshal(task)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).To(ContainSubstring(`"protocolVersion":"v2"`))
}

package svc

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

func TestDesiredObservationDeadlineSweepIsAlwaysRegistered(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("jobs_scheduler.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("DesiredObservationDeadlineSweep"))
	g.Expect(text).To(ContainSubstring("db.RunDesiredObservationDeadlineSweep"))
}

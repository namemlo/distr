package db

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestAppendStepRunSecretRedactionValueIncludesTrimmedVariant(t *testing.T) {
	g := NewWithT(t)

	values := appendStepRunSecretRedactionValue(nil, " secret-value ")

	g.Expect(values).To(Equal([]string{" secret-value ", "secret-value"}))
}

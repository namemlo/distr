package api

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestOperatorPageRequestDefaultsOmittedLimit(t *testing.T) {
	g := NewWithT(t)

	request := OperatorPageRequest{}

	g.Expect(request.Validate()).To(Succeed())
	g.Expect(request.ToPageRequest()).To(Equal(types.PageRequest{Limit: 50}))
}

func TestOperatorPageRequestRejectsExplicitZeroAndOutOfRangeLimits(t *testing.T) {
	for _, limit := range []int{0, -1, 101} {
		t.Run("limit", func(t *testing.T) {
			request := OperatorPageRequest{Limit: new(limit)}

			NewWithT(t).Expect(request.Validate()).To(
				MatchError("limit must be between 1 and 100"),
			)
		})
	}
}

func TestOperatorPageRequestAcceptsMaximumLimit(t *testing.T) {
	limit := 100
	request := OperatorPageRequest{Cursor: "b3BhcXVl", Limit: &limit}

	g := NewWithT(t)
	g.Expect(request.Validate()).To(Succeed())
	g.Expect(request.ToPageRequest()).To(Equal(types.PageRequest{
		Cursor: "b3BhcXVl",
		Limit:  100,
	}))
}

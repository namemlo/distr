package main

import (
	"reflect"
	"testing"
)

func TestDeclaredTestsUsesGoASTAndTestSignatures(t *testing.T) {
	source := []byte(`package proof

import verify "testing"

const decoy = "func TestStringDecoy(t *testing.T) {}"

func TestSelected(t *verify.T) {}
func TestSecond(*verify.T) {}
func TestWrongParameter(t *verify.M) {}
func Testlowercase(t *verify.T) {}
func Helper(t *verify.T) {}

type suite struct{}
func (suite) TestMethod(t *verify.T) {}
`)

	got, err := declaredTests("proof/proof_test.go", source)
	if err != nil {
		t.Fatalf("declaredTests() error = %v", err)
	}
	want := []string{"TestSelected", "TestSecond"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("declaredTests() = %v, want %v", got, want)
	}
}

func TestDeclaredTestsSupportsDotImportedTesting(t *testing.T) {
	source := []byte(`package proof

import . "testing"

func TestSelected(t *T) {}
`)

	got, err := declaredTests("proof/proof_test.go", source)
	if err != nil {
		t.Fatalf("declaredTests() error = %v", err)
	}
	want := []string{"TestSelected"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("declaredTests() = %v, want %v", got, want)
	}
}

func TestDeclaredTestsRejectsShadowedDotImportType(t *testing.T) {
	source := []byte(`package proof

import . "testing"

type T struct{}

func TestDecoy(t *T) {}
`)

	got, err := declaredTests("proof/proof_test.go", source)
	if err != nil {
		t.Fatalf("declaredTests() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("declaredTests() = %v, want no testing.T declarations", got)
	}
}

func TestDeclaredTestsRejectsInvalidGoSource(t *testing.T) {
	_, err := declaredTests("proof/proof_test.go", []byte("package proof\nfunc TestBroken("))
	if err == nil {
		t.Fatal("declaredTests() error = nil, want parse error")
	}
}

func TestRejectBuildConstraintsFindsModernAndLegacyConstraints(t *testing.T) {
	for _, source := range []string{
		"//go:build linux\n\npackage proof\n",
		"// +build linux\n\npackage proof\n",
	} {
		if err := rejectBuildConstraints("proof/proof_test.go", []byte(source)); err == nil {
			t.Fatalf("rejectBuildConstraints() error = nil for %q", source)
		}
	}
}

func TestRejectBuildConstraintsAllowsOrdinaryBoundTestSource(t *testing.T) {
	source := []byte("package proof\n\n//go:build is text inside the package body\n")
	if err := rejectBuildConstraints("proof/proof_test.go", source); err != nil {
		t.Fatalf("rejectBuildConstraints() error = %v", err)
	}
}

package verifier

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/pkg/errors"
)

// Every Err* sentinel declared in errors.go other than ErrVerifierInternal
// is a validation verdict and must be in validationErrors.
func TestValidationErrorsListsEveryVerdict(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "errors.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := 0
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				if strings.HasPrefix(name.Name, "Err") && name.Name != "ErrVerifierInternal" {
					declared++
				}
			}
		}
	}
	if declared == 0 || declared != len(validationErrors) {
		t.Fatalf("validationErrors has %d entries, errors.go declares %d verdicts", len(validationErrors), declared)
	}
}

func TestIsValidationError(t *testing.T) {
	if !IsValidationError(ErrMChangesHashInvalid) {
		t.Fatal("a verdict must be recognised")
	}
	if !IsValidationError(errors.Wrap(ErrABSignatureInvalid, "context")) {
		t.Fatal("a wrapped verdict must be recognised")
	}
	if IsValidationError(InternalError(errors.New("store"))) {
		t.Fatal("an internal failure is not a verdict")
	}
	if IsValidationError(errors.New("leveldb: closed")) || IsValidationError(nil) {
		t.Fatal("an unrelated error or nil is not a verdict")
	}
}

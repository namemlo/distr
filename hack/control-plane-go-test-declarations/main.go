// control-plane-go-test-declarations parses bound Go source with go/ast and
// returns the top-level testing.T test functions declared by each source.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type sourceInput struct {
	Path                   string `json:"path"`
	Content                string `json:"contentBase64"`
	RejectBuildConstraints bool   `json:"rejectBuildConstraints"`
}

type request struct {
	Sources []sourceInput `json:"sources"`
}

type sourceOutput struct {
	Path  string   `json:"path"`
	Tests []string `json:"tests"`
}

type response struct {
	Sources []sourceOutput `json:"sources"`
}

func testingImports(file *ast.File) (aliases map[string]struct{}, dotImported bool) {
	aliases = make(map[string]struct{})
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath != "testing" {
			continue
		}
		if spec.Name == nil {
			aliases["testing"] = struct{}{}
			continue
		}
		switch spec.Name.Name {
		case ".":
			dotImported = true
		case "_":
			// A blank import cannot name testing.T.
		default:
			aliases[spec.Name.Name] = struct{}{}
		}
	}
	if dotImported && file.Scope.Lookup("T") != nil {
		dotImported = false
	}
	return aliases, dotImported
}

func isTestingT(expr ast.Expr, aliases map[string]struct{}, dotImported bool) bool {
	pointer, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch target := pointer.X.(type) {
	case *ast.SelectorExpr:
		qualifier, ok := target.X.(*ast.Ident)
		if !ok || target.Sel.Name != "T" {
			return false
		}
		_, ok = aliases[qualifier.Name]
		return ok
	case *ast.Ident:
		return dotImported && target.Name == "T"
	default:
		return false
	}
}

func isTestName(name string) bool {
	if !strings.HasPrefix(name, "Test") || len(name) == len("Test") {
		return false
	}
	suffix, _ := utf8.DecodeRuneInString(name[len("Test"):])
	return !unicode.IsLower(suffix)
}

func isTopLevelTest(function *ast.FuncDecl, aliases map[string]struct{}, dotImported bool) bool {
	if function.Recv != nil || !isTestName(function.Name.Name) || function.Type.TypeParams != nil {
		return false
	}
	if function.Type.Results != nil && len(function.Type.Results.List) != 0 {
		return false
	}
	parameters := function.Type.Params
	return parameters != nil &&
		len(parameters.List) == 1 &&
		len(parameters.List[0].Names) <= 1 &&
		isTestingT(parameters.List[0].Type, aliases, dotImported)
}

func declaredTests(path string, source []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid Go source: %w", path, err)
	}
	aliases, dotImported := testingImports(file)
	tests := make([]string, 0)
	seen := make(map[string]struct{})
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !isTopLevelTest(function, aliases, dotImported) {
			continue
		}
		if _, duplicate := seen[function.Name.Name]; duplicate {
			return nil, fmt.Errorf("%s declares duplicate top-level test %s", path, function.Name.Name)
		}
		seen[function.Name.Name] = struct{}{}
		tests = append(tests, function.Name.Name)
	}
	return tests, nil
}

func rejectBuildConstraints(path string, source []byte) error {
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(trimmed, "package ") {
			return nil
		}
		if constraint.IsGoBuild(trimmed) || constraint.IsPlusBuild(trimmed) {
			return fmt.Errorf("%s must not use build constraints because it is a bound selected test source", path)
		}
	}
	return nil
}

func run() error {
	var input request
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if len(input.Sources) == 0 {
		return fmt.Errorf("request must contain at least one source")
	}
	output := response{Sources: make([]sourceOutput, 0, len(input.Sources))}
	seenPaths := make(map[string]struct{})
	for _, source := range input.Sources {
		if source.Path == "" {
			return fmt.Errorf("source path must not be empty")
		}
		if _, duplicate := seenPaths[source.Path]; duplicate {
			return fmt.Errorf("source path appears more than once: %s", source.Path)
		}
		seenPaths[source.Path] = struct{}{}
		content, err := base64.StdEncoding.DecodeString(source.Content)
		if err != nil {
			return fmt.Errorf("%s content is not valid base64: %w", source.Path, err)
		}
		if source.RejectBuildConstraints {
			if err := rejectBuildConstraints(source.Path, content); err != nil {
				return err
			}
		}
		tests, err := declaredTests(source.Path, content)
		if err != nil {
			return err
		}
		output.Sources = append(output.Sources, sourceOutput{Path: source.Path, Tests: tests})
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(output)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

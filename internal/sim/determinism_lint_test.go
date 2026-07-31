package sim

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lintTarget is one package the determinism lint guards.
//
// The allowlist on each target is the single place where a deliberate
// exception to the determinism contract is made. Adding an entry is a
// reviewable act: every package listed must be free of ambient state,
// wall-clock, randomness, I/O and iteration-order nondeterminism.
type lintTarget struct {
	directory      string
	name           string
	allowedImports []string
}

// lintTargets are the packages that must stay bit-exact.
//
// internal/stats is here as well as internal/sim because a floating-point
// classifier would break the determinism contract just as thoroughly as a
// floating-point simulation: two machines would agree on every tick of state
// and then file the same run under two different outcomes. Its allowlist
// differs — it needs math/bits for the exact 128-bit product comparison and
// strconv for its assertion messages, and it has no reason to touch
// encoding/binary or sort.
var lintTargets = []lintTarget{
	{
		directory:      ".",
		name:           "internal/sim",
		allowedImports: []string{"math/bits", "encoding/binary", "sort", "errors"},
	},
	{
		directory:      "../stats",
		name:           "internal/stats",
		allowedImports: []string{"math/bits", "strconv"},
	},
}

// TestDeterminismLint walks the AST of every non-test file in the guarded
// packages and rejects the constructs that can silently break bit-exact
// reproducibility:
//
//   - maps of any kind        — iteration order is randomised by the runtime
//   - float32 / float64       — FMA fusion differs between arm64 and amd64
//   - goroutines / select / channels — scheduling order is nondeterministic
//   - imports outside the allowlist  — ambient state, wall-clock, randomness, I/O
//
// Pure go/ast, no type information required, so it stays fast and dependency
// free. Test files are exempt: they legitimately need testing, os, fmt,
// reflect, and — for the numerics references — math/big and math/rand.
func TestDeterminismLint(t *testing.T) {
	for _, target := range lintTargets {
		t.Run(target.name, func(t *testing.T) {
			lintPackage(t, target)
		})
	}
}

func lintPackage(t *testing.T, target lintTarget) {
	t.Helper()

	entries, err := os.ReadDir(target.directory)
	if err != nil {
		t.Fatalf("read %s: %v", target.name, err)
	}

	allowed := make(map[string]bool, len(target.allowedImports))
	for _, path := range target.allowedImports {
		allowed[path] = true
	}
	allowlist := strings.Join(target.allowedImports, ", ")

	fileSet := token.NewFileSet()
	inspected := 0

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		path := filepath.Join(target.directory, name)
		file, err := parser.ParseFile(fileSet, path, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		inspected++

		fail := func(node ast.Node, message string) {
			t.Errorf("%s: %s", fileSet.Position(node.Pos()), message)
		}

		for _, importSpec := range file.Imports {
			imported := strings.Trim(importSpec.Path.Value, `"`)
			if !allowed[imported] {
				fail(importSpec, "import "+imported+" is not in the "+target.name+" allowlist ("+
					allowlist+") — see the determinism contract")
			}
		}

		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.MapType:
				fail(typed, "map type in "+target.name+" — map iteration order is nondeterministic; "+
					"use a dense id-indexed slice or a sorted slice with binary search")
			case *ast.ChanType:
				fail(typed, "channel type in "+target.name+" — this package is single-threaded")
			case *ast.GoStmt:
				fail(typed, "go statement in "+target.name+" — this package is single-threaded; "+
					"parallelism belongs in internal/runner, between runs")
			case *ast.SelectStmt:
				fail(typed, "select statement in "+target.name+" — this package is single-threaded")
			case *ast.SendStmt:
				fail(typed, "channel send in "+target.name+" — this package is single-threaded")
			case *ast.Ident:
				if typed.Name == "float32" || typed.Name == "float64" {
					fail(typed, "identifier "+typed.Name+" in "+target.name+" — integer arithmetic only; "+
						"floats may be FMA-fused differently per architecture")
				}
			case *ast.UnaryExpr:
				if typed.Op == token.ARROW {
					fail(typed, "channel receive in "+target.name+" — this package is single-threaded")
				}
			case *ast.CallExpr:
				// Redundant with *ast.MapType (make's first argument is a type
				// expression) but stated explicitly so the intent survives any
				// future refactor of this walker.
				ident, isIdent := typed.Fun.(*ast.Ident)
				if isIdent && ident.Name == "make" && len(typed.Args) > 0 {
					if _, isMap := typed.Args[0].(*ast.MapType); isMap {
						fail(typed, "make(map[...]) in "+target.name+" — maps are forbidden")
					}
				}
			}
			return true
		})
	}

	if inspected == 0 {
		t.Fatalf("determinism lint inspected 0 files — the walker is not seeing %s", target.name)
	}
}

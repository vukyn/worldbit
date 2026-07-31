package sim

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// allowedImports is the complete set of packages internal/sim may import.
//
// This list is the single place where a deliberate exception to the
// determinism contract is made. Adding an entry is a reviewable act: every
// package here must be free of ambient state, wall-clock, randomness, I/O and
// iteration-order nondeterminism.
var allowedImports = map[string]bool{
	"math/bits":       true,
	"encoding/binary": true,
	"sort":            true,
	"errors":          true,
}

// TestDeterminismLint walks the AST of every non-test file in internal/sim and
// rejects the constructs that can silently break bit-exact reproducibility:
//
//   - maps of any kind        — iteration order is randomised by the runtime
//   - float32 / float64       — FMA fusion differs between arm64 and amd64
//   - goroutines / select / channels — scheduling order is nondeterministic
//   - imports outside the allowlist  — ambient state, wall-clock, randomness, I/O
//
// Pure go/ast, no type information required, so it stays fast and dependency
// free. Test files are exempt: they legitimately need testing, os, fmt, reflect.
func TestDeterminismLint(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/sim: %v", err)
	}

	fileSet := token.NewFileSet()
	inspected := 0

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fileSet, name, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		inspected++

		fail := func(node ast.Node, message string) {
			t.Errorf("%s: %s", fileSet.Position(node.Pos()), message)
		}

		for _, importSpec := range file.Imports {
			path := strings.Trim(importSpec.Path.Value, `"`)
			if !allowedImports[path] {
				fail(importSpec, "import "+path+" is not in the internal/sim allowlist "+
					"(math/bits, encoding/binary, sort, errors) — see the determinism contract")
			}
		}

		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.MapType:
				fail(typed, "map type in internal/sim — map iteration order is nondeterministic; "+
					"use a dense id-indexed slice or a sorted slice with binary search")
			case *ast.ChanType:
				fail(typed, "channel type in internal/sim — the simulation is single-threaded")
			case *ast.GoStmt:
				fail(typed, "go statement in internal/sim — the simulation is single-threaded; "+
					"parallelism belongs in internal/runner, between runs")
			case *ast.SelectStmt:
				fail(typed, "select statement in internal/sim — the simulation is single-threaded")
			case *ast.SendStmt:
				fail(typed, "channel send in internal/sim — the simulation is single-threaded")
			case *ast.Ident:
				if typed.Name == "float32" || typed.Name == "float64" {
					fail(typed, "identifier "+typed.Name+" in internal/sim — integer arithmetic only; "+
						"floats may be FMA-fused differently per architecture")
				}
			case *ast.UnaryExpr:
				if typed.Op == token.ARROW {
					fail(typed, "channel receive in internal/sim — the simulation is single-threaded")
				}
			case *ast.CallExpr:
				// Redundant with *ast.MapType (make's first argument is a type
				// expression) but stated explicitly so the intent survives any
				// future refactor of this walker.
				ident, isIdent := typed.Fun.(*ast.Ident)
				if isIdent && ident.Name == "make" && len(typed.Args) > 0 {
					if _, isMap := typed.Args[0].(*ast.MapType); isMap {
						fail(typed, "make(map[...]) in internal/sim — maps are forbidden")
					}
				}
			}
			return true
		})
	}

	if inspected == 0 {
		t.Fatal("determinism lint inspected 0 files — the walker is not seeing internal/sim")
	}
}

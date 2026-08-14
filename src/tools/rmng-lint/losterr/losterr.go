// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package losterr reports errors that are constructed and then thrown away.
//
// Constructing an error is a statement that something went wrong. Discarding the value turns that
// statement into silence, and the caller is told the operation succeeded.
//
// The check is deliberately narrow: it fires only when a constructor's result is used for nothing
// at all. Assigning to `_`, logging, wrapping and returning are all left alone.
package losterr

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "losterr",
	Doc:      "reports error values that are constructed and immediately discarded, which reports failure as success",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// errorConstructors are the calls whose only purpose is to produce an error value. A bare call
// statement to any of them cannot do anything else, so the result is necessarily lost.
var errorConstructors = map[string]bool{
	"rmerror.NewRMError": true,
	"fmt.Errorf":         true,
	"errors.New":         true,
	"errors.Join":        true,
}

func calleeName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + sel.Sel.Name
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// An ExprStmt is an expression evaluated purely for its side effects. An error constructor has
	// none, so finding one here means the value went nowhere.
	insp.Preorder([]ast.Node{(*ast.ExprStmt)(nil)}, func(n ast.Node) {
		stmt := n.(*ast.ExprStmt)
		call, ok := stmt.X.(*ast.CallExpr)
		if !ok {
			return
		}
		name := calleeName(call)
		if !errorConstructors[name] {
			return
		}
		pass.Reportf(stmt.Pos(),
			"%s result is discarded: the error is built and thrown away, so the caller sees success. Return it, or drop the call.",
			name)
	})

	return nil, nil
}

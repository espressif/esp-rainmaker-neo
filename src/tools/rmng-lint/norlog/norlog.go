// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package norlog

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "norlog",
	Doc:      "reports rlog.Error(nil)/Info(nil)/etc calls in functions that have a context parameter",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

var ctxFuncs = map[string]bool{
	"Err":       true,
	"Trace":     true,
	"Debug":     true,
	"Info":      true,
	"Warn":      true,
	"Error":     true,
	"Fatal":     true,
	"Panic":     true,
	"WithLevel": true,
	"Log":       true,
	"Print":     true,
	"Printf":    true,
}

func isRlogPackage(path string) bool {
	return path == "github.com/espressif/esp-rainmaker-neo/src/utils/rlog" || strings.HasSuffix(path, "/rlog")
}

var contextIfacePath = "context.Context"

func implementsContext(t types.Type, ctxIface *types.Interface) bool {
	return types.Implements(t, ctxIface) || types.Implements(types.NewPointer(t), ctxIface)
}

func funcHasContext(pass *analysis.Pass, fnType *ast.FuncType) bool {
	if fnType == nil || fnType.Params == nil {
		return false
	}

	ctxObj := pass.Pkg.Imports()
	var ctxIface *types.Interface
	for _, imp := range ctxObj {
		if imp.Path() == "context" {
			obj := imp.Scope().Lookup("Context")
			if obj != nil {
				ctxIface, _ = obj.Type().Underlying().(*types.Interface)
			}
			break
		}
	}
	if ctxIface == nil {
		return false
	}

	for _, field := range fnType.Params.List {
		t := pass.TypesInfo.TypeOf(field.Type)
		if t == nil {
			continue
		}
		if implementsContext(t, ctxIface) {
			return true
		}
	}
	return false
}

func run(pass *analysis.Pass) (interface{}, error) {
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	funcStack := make([]*ast.FuncType, 0, 8)

	ins.WithStack(nil, func(n ast.Node, push bool, stack []ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			if push {
				funcStack = append(funcStack, node.Type)
			} else {
				funcStack = funcStack[:len(funcStack)-1]
			}
		case *ast.FuncLit:
			if push {
				funcStack = append(funcStack, node.Type)
			} else {
				funcStack = funcStack[:len(funcStack)-1]
			}
		case *ast.CallExpr:
			if !push {
				return true
			}
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if !ctxFuncs[sel.Sel.Name] {
				return true
			}

			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}

			obj := pass.TypesInfo.Uses[ident]
			if obj == nil {
				return true
			}

			pkgName, ok := obj.(*types.PkgName)
			if !ok {
				return true
			}

			if !isRlogPackage(pkgName.Imported().Path()) {
				return true
			}

			if len(node.Args) == 0 {
				return true
			}
			argIdent, ok := node.Args[0].(*ast.Ident)
			if !ok || argIdent.Name != "nil" {
				return true
			}

			if len(funcStack) > 0 && funcHasContext(pass, funcStack[len(funcStack)-1]) {
				pass.Reportf(node.Pos(), "pass context to rlog.%s() instead of nil", sel.Sel.Name)
			}
		}
		return true
	})

	return nil, nil
}

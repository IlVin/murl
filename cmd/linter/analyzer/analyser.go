package analyzer

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "paniclogexitchecker",
	Doc:  "check for misuse of panic, log.Fatal, and os.Exit",
	Run:  run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	for _, file := range pass.Files {
		packageName := file.Name.Name
		isMainPkg := packageName == "main"
		isMainFunc := false

		// Проверяем, есть ли функция main в этом файле и находимся ли мы внутри неё
		// Для этого будем проверять каждый вызов в контексте функции main

		ast.Inspect(file, func(n ast.Node) bool {
			// Отслеживаем, находимся ли мы в функции main
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Name.Name == "main" && isMainPkg {
					isMainFunc = true
					defer func() { isMainFunc = false }()
				} else {
					isMainFunc = false
				}
			}

			// Проверка вызовов
			if call, ok := n.(*ast.CallExpr); ok {
				// 1. panic - всегда запрещён (согласно заданию)
				if fun, ok := call.Fun.(*ast.Ident); ok && fun.Name == "panic" {
					pass.Reportf(call.Pos(), "avoid using panic in production code")
					return true
				}

				// 2. Проверка на log.Fatal и os.Exit
				if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
					if x, ok := fun.X.(*ast.Ident); ok {
						// log.Fatal, log.Fatalf, log.Fatalln
						if x.Name == "log" && strings.HasPrefix(fun.Sel.Name, "Fatal") {
							if !isMainPkg || !isMainFunc {
								pass.Reportf(call.Pos(), "log.%s should only be used in main.main", fun.Sel.Name)
							}
						}
						// os.Exit
						if x.Name == "os" && fun.Sel.Name == "Exit" {
							if !isMainPkg || !isMainFunc {
								pass.Reportf(call.Pos(), "os.Exit should only be used in main.main")
							}
						}
					}
				}
			}
			return true
		})
	}
	return nil, nil
}

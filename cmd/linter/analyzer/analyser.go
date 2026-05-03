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
		// Пропускаем тестовые файлы
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}

		packageName := file.Name.Name
		isMainPkg := packageName == "main"

		// Флаг, указывающий, находимся ли мы внутри функции main()
		inMainMain := false

		ast.Inspect(file, func(n ast.Node) bool {
			// Если узел nil — мы выходим из обхода текущей ветки
			if n == nil {
				return true
			}

			// Отслеживаем вход и выход из деклараций функций
			if fn, ok := n.(*ast.FuncDecl); ok {
				// Проверяем, является ли функция main() в пакете main
				if isMainPkg && fn.Name.Name == "main" && fn.Recv == nil {
					inMainMain = true

					// Обходим тело функции main вручную, чтобы точно контролировать контекст
					if fn.Body != nil {
						ast.Inspect(fn.Body, func(child ast.Node) bool {
							if child == nil {
								return true
							}
							checkCall(pass, child, true)
							return true
						})
					}

					inMainMain = false
					// Возвращаем false, чтобы ast.Inspect не обходил эту функцию повторно
					return false
				}
			}

			// Проверяем вызовы вне функции main.main
			checkCall(pass, n, inMainMain)
			return true
		})
	}
	return nil, nil
}

// Выделенная функция для проверки вызовов
func checkCall(pass *analysis.Pass, n ast.Node, inMainMain bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	// 1. panic - всегда запрещён
	if fun, ok := call.Fun.(*ast.Ident); ok && fun.Name == "panic" {
		pass.Reportf(call.Pos(), "avoid using panic in production code")
		return
	}

	// 2. Проверка на log.Fatal и os.Exit
	if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
		if x, ok := fun.X.(*ast.Ident); ok {
			// log.Fatal, log.Fatalf, log.Fatalln
			if x.Name == "log" && strings.HasPrefix(fun.Sel.Name, "Fatal") {
				if !inMainMain {
					pass.Reportf(call.Pos(), "log.%s should only be used in main.main", fun.Sel.Name)
				}
			}

			// os.Exit
			if x.Name == "os" && fun.Sel.Name == "Exit" {
				if !inMainMain {
					pass.Reportf(call.Pos(), "os.Exit should only be used in main.main")
				}
			}
		}
	}
}

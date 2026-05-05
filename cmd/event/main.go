package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

type EvtType struct {
	Evt  string
	Type string
}

type Data struct {
	Pkg    string
	Events []EvtType
}

const Template = `package {{.Pkg}}` + "\n" +
	`{{range .Events}}` +
	`func ({{.Type}}) EventType() EvType { return EvType("{{.Evt}}") }` + "\n" +
	`{{end}}`

func main() {
	// vals := make(map[string]string)

	for _, val := range os.Args[1:] {
		if !strings.HasSuffix(val, ".go") {
			continue
		}

		srcPath, dstPath, err := FilePath(val)
		if err != nil {
			panic(err)
		}

		// Пустой набор файлов
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, srcPath, nil, parser.ParseComments)
		if err != nil {
			panic(err)
		}

		pkgName := f.Name.Name
		eventPayloads := FindEventStructs(f)
		if len(eventPayloads) > 0 {
			t := template.Must(template.New("list").Parse(Template))
			data := Data{
				Pkg:    pkgName,
				Events: eventPayloads,
			}
			var buf bytes.Buffer
			err := t.Execute(&buf, data)
			if err != nil {
				panic(err)
			}
			bufFmt, err := format.Source(buf.Bytes())
			if err != nil {
				panic(err)
			}
			err = os.WriteFile(dstPath, bufFmt, 0644)
			if err != nil {
				panic(err)
			}
		}
	}
}

// Ищем типы, перед которыми размещен тэг //go:event
func FindEventStructs(f *ast.File) []EvtType {
	eventPayloads := []EvtType{}
	for _, decl := range f.Decls {
		switch genDecl := decl.(type) {
		case *ast.GenDecl:
			if genDecl.Tok == token.TYPE {
				if genDecl.Doc != nil {
					for _, comment := range genDecl.Doc.List {
						tag_fields := strings.Fields(comment.Text)
						if tag_fields[0] == "//go:event" {
							for _, spec := range genDecl.Specs {
								typeSpec := spec.(*ast.TypeSpec)
								if _, ok := typeSpec.Type.(*ast.StructType); ok {
									if len(tag_fields) > 1 {
										eventPayloads = append(eventPayloads, EvtType{Type: typeSpec.Name.Name, Evt: tag_fields[1]})
									} else {
										eventPayloads = append(eventPayloads, EvtType{Type: typeSpec.Name.Name, Evt: "Ev" + typeSpec.Name.Name})
									}
								}
							}
							break
						}
					}
				}
			}
		}
	}
	return eventPayloads
}

// Вычисляет файл исходник и файл приемник
func FilePath(srcName string) (string, string, error) {
	if !strings.HasSuffix(srcName, ".go") {
		return "", "", fmt.Errorf("no go filename: %s", srcName)
	}

	dir := os.Getenv("PWD")
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", "", err
		}
	}

	return filepath.Join(dir, srcName), filepath.Join(dir, srcName[:len(srcName)-3]+"_events.go"), nil
}

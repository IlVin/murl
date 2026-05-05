package main

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateReset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reset_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// ВАЖНО: Добавляем нормальные отступы и комментарии для AST
	sourceCode := `package testpkg

// generate:reset
type TestStruct struct {
	IntField    int
	StringField string
	PtrField    *string
	SliceField  []int
	MapField    map[string]int
	Child       *TestStruct
}
`
	// Пишем файл
	err = os.WriteFile(filepath.Join(tmpDir, "data.go"), []byte(sourceCode), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Вызываем функцию генерации напрямую для этой папки
	err = generateResetForPackage(tmpDir)
	if err != nil {
		t.Fatalf("Generation failed: %v", err)
	}

	genPath := filepath.Join(tmpDir, "reset.gen.go")

	// Проверяем существование файла
	if _, err := os.Stat(genPath); os.IsNotExist(err) {
		// Если файла нет, выведем содержимое папки для отладки
		files, _ := os.ReadDir(tmpDir)
		var names []string
		for _, f := range files {
			names = append(names, f.Name())
		}
		t.Fatalf("reset.gen.go was not generated. Files in dir: %v", names)
	}

	content, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatal(err)
	}

	sContent := string(content)
	expectedParts := []string{
		"func (s *TestStruct) Reset()",
		"s.IntField = 0",
		"s.StringField = \"\"",
		"if s.PtrField != nil",
		"*s.PtrField = \"\"",
		"s.SliceField = s.SliceField[:0]",
		"clear(s.MapField)",
	}

	for _, part := range expectedParts {
		if !strings.Contains(sContent, part) {
			t.Errorf("Generated code missing expected part: %s\nFull content:\n%s", part, sContent)
		}
	}
}

func TestIsBasicType(t *testing.T) {
	tests := []struct {
		typeName string
		expected bool
	}{
		{"int", true},
		{"string", true},
		{"bool", true},
		{"MyCustomStruct", false},
		{"float64", true},
	}

	for _, tt := range tests {
		if res := isBasicType(tt.typeName); res != tt.expected {
			t.Errorf("isBasicType(%s) = %v, want %v", tt.typeName, res, tt.expected)
		}
	}
}

func TestGetZeroValue(t *testing.T) {
	if v := getZeroValue("string"); v != `""` {
		t.Errorf("Expected empty string literal, got %s", v)
	}
	if v := getZeroValue("int"); v != "0" {
		t.Errorf("Expected 0, got %s", v)
	}
}

func TestGenerateResetCodeLogic(t *testing.T) {
	tests := []struct {
		name     string
		field    FieldInfo
		contains string
	}{
		{
			"Basic int",
			FieldInfo{Name: "Age", TypeName: "int"},
			"s.Age = 0",
		},
		{
			"Pointer string",
			FieldInfo{Name: "Name", TypeName: "string", IsPtr: true},
			"*s.Name = \"\"",
		},
		{
			"Slice",
			FieldInfo{Name: "List", IsSlice: true},
			"s.List = s.List[:0]",
		},
		{
			"Map",
			FieldInfo{Name: "Dict", IsMap: true},
			"clear(s.Dict)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := generateResetCode(tt.field, "s")
			if !strings.Contains(res, tt.contains) {
				t.Errorf("Result %s does not contain %s", res, tt.contains)
			}
		})
	}
}

// TestFullGeneratorCoverage проверяет все типы полей, включая селекторы,
// вложенные указатели и специфические типы для покрытия parseFieldType.
func TestFullGeneratorCoverage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reset_full_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Создаем файл, который заставит пройти по всем веткам switch-case
	sourceCode := `
package testpkg
import "time"

// generate:reset
type AllTypesStruct struct {
	IntField      int
	StringPtr     *string
	SlicePtr      *[]int
	MapPtr        *map[string]string
	StructPtr     *time.Time
	TimeField     time.Time
	Interface     interface{}
	SimpleSlice   []string
	SimpleMap     map[int]int
}

// Структура без тега — должна быть проигнорирована
type Ignored struct {
	A int
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "all_types.go"), []byte(sourceCode), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Тест успешной генерации
	err = generateResetForPackage(tmpDir)
	if err != nil {
		t.Fatalf("Generation failed: %v", err)
	}

	genContent, err := os.ReadFile(filepath.Join(tmpDir, "reset.gen.go"))
	if err != nil {
		t.Fatal("reset.gen.go not found")
	}

	sContent := string(genContent)
	checks := []string{
		"s.IntField = 0",
		"*s.StringPtr = \"\"",
		"if s.StructPtr != nil",
		"clear(*s.MapPtr)",
		"s.TimeField = time.Time{}",
		"s.SimpleSlice = s.SimpleSlice[:0]",
		"clear(s.SimpleMap)",
	}

	for _, check := range checks {
		if !strings.Contains(sContent, check) {
			t.Errorf("Missing expected code: %s", check)
		}
	}

	if strings.Contains(sContent, "func (s *Ignored) Reset") {
		t.Error("Generated Reset for struct without tag")
	}
}

// TestEmptyAndHiddenDirs проверяет обход пустых папок и игнорирование vendor/.
func TestEmptyAndHiddenDirs(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "reset_empty_test")
	defer os.RemoveAll(tmpDir)

	// Создаем папку vendor (должна быть пропущена)
	vendorPath := filepath.Join(tmpDir, "vendor")
	os.Mkdir(vendorPath, 0755)
	os.WriteFile(filepath.Join(vendorPath, "data.go"), []byte("package vendor\n // generate:reset\n type T struct{}"), 0644)

	// Создаем скрытую папку (должна быть пропущена)
	dotPath := filepath.Join(tmpDir, ".git")
	os.Mkdir(dotPath, 0755)

	// Запускаем через main-логику (filepath.Walk)
	os.Args = []string{"cmd", tmpDir}
	main() // Не должно упасть и не должно создать reset.gen.go в vendor

	filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if strings.Contains(path, "reset.gen.go") {
			t.Errorf("Should not generate file in vendor or hidden dir: %s", path)
		}
		return nil
	})
}

// TestSyntaxError покрытие случая, когда format.Source получает битый код.
func TestSyntaxError(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "reset_syntax_test")
	defer os.RemoveAll(tmpDir)

	// Специально создаем ситуацию, которая может привести к битому коду
	// (например, структура с некорректным именем для Go)
	data := Data{
		Pkg: "bad",
		Structs: []StructInfo{
			{Name: "Broken-Struct", Fields: []FieldInfo{{Name: "A", TypeName: "int"}}},
		},
	}

	var buf bytes.Buffer
	resetTemplate.Execute(&buf, data)

	// format.Source должен выдать ошибку на "Broken-Struct"
	_, err := format.Source(buf.Bytes())
	if err == nil {
		t.Error("Expected syntax error for 'Broken-Struct', but got none")
	}
}

// TestHelperFunctions покрытие мелких вспомогательных функций.
func TestHelpers(t *testing.T) {
	if !isBasicType("uint64") {
		t.Error("uint64 should be basic type")
	}
	if isBasicType("MyType") {
		t.Error("MyType should not be basic type")
	}
	if getZeroValue("float32") != "0.0" {
		t.Error("Wrong zero value for float")
	}
}

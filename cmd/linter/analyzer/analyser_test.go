package analyzer

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	// analysistest требует создания тестовых файлов в testdata/src/a
	// Аргумент "a" - это путь относительно testdata/src
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, Analyzer, "a")
}

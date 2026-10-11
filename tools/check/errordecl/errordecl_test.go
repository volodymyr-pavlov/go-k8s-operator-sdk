package errordecl

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestErrorDecl(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "example")
}

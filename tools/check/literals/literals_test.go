package literals

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestLiterals(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "example")
}

func TestFixExpandsTheLiteral(t *testing.T) {
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), Analyzer, "fixed")
}

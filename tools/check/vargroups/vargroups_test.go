package vargroups

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestVarGroups(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "example")
}

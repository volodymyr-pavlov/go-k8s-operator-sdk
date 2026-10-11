// Command check runs the style checks of the repository: each composite literal has a line for each field, each
// package-level var stands on its own, and every error is declared in errors.go. It takes the same arguments as other analysis programs, such as the package
// patterns, and with -fix it rewrites the code that the literals check finds.
//
//	cd apps/backend && ../../.bin/check ./...
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/volodymyr-pavlov/go-k8s-operator-sdk/tools/check/errordecl"
	"github.com/volodymyr-pavlov/go-k8s-operator-sdk/tools/check/literals"
	"github.com/volodymyr-pavlov/go-k8s-operator-sdk/tools/check/vargroups"
)

func main() {
	multichecker.Main(
		errordecl.Analyzer,
		literals.Analyzer,
		vargroups.Analyzer,
	)
}

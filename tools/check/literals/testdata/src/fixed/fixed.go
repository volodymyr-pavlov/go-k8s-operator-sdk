package fixed

type pair struct {
	a, b int
}

var crowded = pair{a: 1, b: 2} // want `several fields on one line`

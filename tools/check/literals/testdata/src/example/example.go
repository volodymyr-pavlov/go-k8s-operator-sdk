package example

type pair struct {
	a, b int
}

var crowded = pair{a: 1, b: 2} // want `several fields on one line`

var spread = pair{
	a: 1,
	b: 2,
}

var single = pair{a: 1}

var values = []int{1, 2, 3} // want `several fields on one line`

var nested = []pair{{a: 1, b: 2}, {a: 3, b: 4}} // want `several fields on one line`

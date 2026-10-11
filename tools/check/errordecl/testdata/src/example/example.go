package example

import "errors"

var errLate = errors.New("late") // want `the error errLate belongs in errors.go` `errors.New belongs in errors.go`

var notAnError = 1

func inside() error {
	return errors.New("inside") // want `errors.New belongs in errors.go`
}

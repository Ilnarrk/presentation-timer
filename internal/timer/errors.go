package timer

import "errors"

var (
	ErrInvalidTransition = errors.New("invalid phase transition")
	ErrInvalidDuration   = errors.New("duration must be greater than zero")
)

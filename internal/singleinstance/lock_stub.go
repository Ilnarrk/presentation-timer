//go:build !windows

package singleinstance

type Lock struct{}

func Acquire() (*Lock, error) {
	return &Lock{}, nil
}

func (*Lock) Close() error {
	return nil
}

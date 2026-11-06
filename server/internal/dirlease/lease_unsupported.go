//go:build !darwin && !linux

package dirlease

import "errors"

var ErrUnavailable = errors.New("data directory leases require macOS or Linux")

type Lease struct{}

func Acquire(string) (*Lease, error)                   { return nil, ErrUnavailable }
func (*Lease) Within(string, func(string) error) error { return ErrUnavailable }
func (*Lease) Close() error                            { return nil }

//go:build darwin || linux

package main

import "github.com/letrahoo/monee/server/internal/dirlease"

func lockDirectory(dir string) (func(), error) {
	lease, err := dirlease.Acquire(dir)
	if err != nil {
		return nil, err
	}
	return func() { _ = lease.Close() }, nil
}

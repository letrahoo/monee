//go:build darwin || linux

// Package dirlease coordinates cooperating Monee processes via service.lock.
// It cannot exclude arbitrary SQLite clients or database aliases in other dirs.
package dirlease

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var ErrUnavailable = errors.New("data directory lease unavailable")

// Lease must not be copied. Close waits for a running Within operation.
// The lock file is never removed: unlinking it would split the lock domain.
type Lease struct {
	mu        sync.Mutex
	file      *os.File
	directory string
}

func canonical(dir string) (string, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return "", ErrUnavailable
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", ErrUnavailable
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", ErrUnavailable
	}
	return path, nil
}

// Acquire is nonblocking and does not create the target directory.
// Preserve the service.lock filename/flock protocol used by older services.
func Acquire(dir string) (*Lease, error) {
	path, err := canonical(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(path, "service.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, ErrUnavailable
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrUnavailable
	}
	l := &Lease{file: f, directory: path}
	if !l.valid() {
		l.Close()
		return nil, ErrUnavailable
	}
	return l, nil
}

func (l *Lease) valid() bool {
	if l.file == nil {
		return false
	}
	held, err := l.file.Stat()
	if err != nil || !held.Mode().IsRegular() {
		return false
	}
	named, err := os.Lstat(filepath.Join(l.directory, "service.lock"))
	return err == nil && named.Mode().IsRegular() && os.SameFile(held, named)
}

// Within runs a scoped operation while this lease is live. It rejects a lease
// for another directory and serializes Close. fn must not reenter this lease.
func (l *Lease) Within(dir string, fn func(canonicalDirectory string) error) error {
	if l == nil || fn == nil {
		return ErrUnavailable
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	path, err := canonical(dir)
	if err != nil || path != l.directory || !l.valid() {
		return ErrUnavailable
	}
	return fn(l.directory)
}

func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	unlock := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	closeErr := f.Close()
	if unlock != nil {
		return unlock
	}
	return closeErr
}

package redaction

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// LoadOrCreate keeps aliases stable across restarts. The service holds the data
// directory lock before calling this function. Existing invalid keys are never
// replaced: silently rotating would break evidence linkage.
func LoadOrCreate(dataDir string) (*Projector, error) {
	dir, err := os.Lstat(dataDir)
	if err != nil || !dir.IsDir() || dir.Mode().Perm() != 0700 {
		return nil, errors.New("redaction key directory must be private")
	}
	path := filepath.Join(dataDir, "redaction.key")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, errors.New("cannot generate redaction key")
		}
		file, err := os.CreateTemp(dataDir, ".redaction-key-*")
		if err != nil {
			return nil, errors.New("cannot create redaction key")
		}
		temp := file.Name()
		defer os.Remove(temp)
		if _, err = file.Write(key); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return nil, errors.New("cannot persist redaction key")
		}
		// Link publishes an already complete private file without replacing a key
		// created by another process. A crash before this step leaves no partial key.
		if err = os.Link(temp, path); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, errors.New("cannot publish redaction key")
		}
		parent, err := os.Open(dataDir)
		if err != nil {
			return nil, errors.New("cannot sync redaction key directory")
		}
		err = parent.Sync()
		closeErr = parent.Close()
		if err != nil || closeErr != nil {
			return nil, errors.New("cannot sync redaction key directory")
		}
	} else if err != nil {
		return nil, errors.New("cannot inspect redaction key")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errors.New("redaction key must be a private regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read redaction key")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0600 {
		return nil, errors.New("redaction key changed while opening")
	}
	key, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid persisted redaction key")
	}
	return New(key)
}

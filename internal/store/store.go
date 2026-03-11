package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pipe/internal/fsx"
	"pipe/internal/hash"
)

type StoredObject struct {
	ObjectRef string
	Path      string
	Kind      string
	SizeBytes int64
}

type Store struct {
	root string
}

func New(root string) *Store {
	return &Store{root: filepath.Join(root, ".pipe", "objects", "sha256")}
}

func (s *Store) StoreBytes(data []byte) (StoredObject, error) {
	sum := hash.Bytes(data)
	dst := s.objectPath(sum)
	if _, err := os.Stat(dst); err != nil {
		if !os.IsNotExist(err) {
			return StoredObject{}, err
		}
		if err := fsx.AtomicWriteFile(dst, data, 0o644); err != nil {
			return StoredObject{}, err
		}
	}
	return StoredObject{
		ObjectRef: "sha256:" + sum,
		Path:      dst,
		Kind:      "file",
		SizeBytes: int64(len(data)),
	}, nil
}

func (s *Store) StoreArtifact(path, kind string) (StoredObject, error) {
	switch kind {
	case "file":
		sum, size, err := hash.File(path)
		if err != nil {
			return StoredObject{}, err
		}
		dst := s.objectPath(sum)
		if _, err := os.Stat(dst); err != nil {
			if !os.IsNotExist(err) {
				return StoredObject{}, err
			}
			info, err := os.Stat(path)
			if err != nil {
				return StoredObject{}, err
			}
			if err := fsx.CopyFile(path, dst, info.Mode()); err != nil {
				return StoredObject{}, err
			}
		}
		return StoredObject{ObjectRef: "sha256:" + sum, Path: dst, Kind: kind, SizeBytes: size}, nil
	case "dir":
		sum, size, err := hash.Dir(path)
		if err != nil {
			return StoredObject{}, err
		}
		dst := s.objectPath(sum)
		if _, err := os.Stat(dst); err != nil {
			if !os.IsNotExist(err) {
				return StoredObject{}, err
			}
			if err := fsx.CopyDir(path, dst); err != nil {
				return StoredObject{}, err
			}
		}
		return StoredObject{ObjectRef: "sha256:" + sum, Path: dst, Kind: kind, SizeBytes: size}, nil
	default:
		return StoredObject{}, fmt.Errorf("unsupported artifact type %q", kind)
	}
}

func (s *Store) Resolve(objectRef string) (string, error) {
	if !strings.HasPrefix(objectRef, "sha256:") {
		return "", fmt.Errorf("unsupported object ref %q", objectRef)
	}
	path := s.objectPath(strings.TrimPrefix(objectRef, "sha256:"))
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) objectPath(sum string) string {
	prefix := sum
	if len(prefix) >= 2 {
		prefix = prefix[:2]
	}
	return filepath.Join(s.root, prefix, sum)
}

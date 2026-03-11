package fsx

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func AtomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func CopyFile(src, dst string, mode fs.FileMode) error {
	if err := EnsureDir(filepath.Dir(dst)); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if d.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := EnsureDir(filepath.Dir(target)); err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}
		return CopyFile(path, target, info.Mode())
	})
}

func RemoveIfExists(path string) error {
	err := os.RemoveAll(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func SafeJoin(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", errors.New("absolute paths are not allowed")
	}
	cleaned := filepath.Clean(rel)
	if cleaned == ".." || cleaned == "." && rel == ".." {
		return "", errors.New("path escapes root")
	}
	full := filepath.Join(root, cleaned)
	relToRoot, err := filepath.Rel(root, full)
	if err != nil {
		return "", err
	}
	if relToRoot == ".." || len(relToRoot) >= 3 && relToRoot[:3] == ".."+string(filepath.Separator) {
		return "", errors.New("path escapes root")
	}
	return full, nil
}

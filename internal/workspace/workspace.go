package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"pakkun/internal/fsx"
)

type Mode string

const (
	ModeSymlink Mode = "symlink"
	ModeCopy    Mode = "copy"
)

var (
	ErrTargetExists   = errors.New("target already exists")
	ErrTargetNotEmpty = errors.New("target directory is not empty")
)

func EnsureEmptyDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(path, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrTargetExists, path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %s", ErrTargetNotEmpty, path)
	}
	return nil
}

func Materialize(source, target string, mode Mode) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if err := prepareTarget(target, info.IsDir()); err != nil {
		return err
	}
	if err := fsx.EnsureDir(filepath.Dir(target)); err != nil {
		return err
	}
	switch mode {
	case ModeCopy:
		if info.IsDir() {
			return fsx.CopyDir(source, target)
		}
		return fsx.CopyFile(source, target, info.Mode())
	case ModeSymlink:
		if err := os.Symlink(source, target); err == nil {
			return nil
		}
		if info.IsDir() {
			return fsx.CopyDir(source, target)
		}
		return fsx.CopyFile(source, target, info.Mode())
	default:
		return fmt.Errorf("unsupported projection mode %q", mode)
	}
}

func prepareTarget(target string, sourceIsDir bool) error {
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.IsDir() || sourceIsDir {
		return fmt.Errorf("%w: %s", ErrTargetExists, target)
	}
	if err := os.Remove(target); err != nil {
		return err
	}
	return nil
}

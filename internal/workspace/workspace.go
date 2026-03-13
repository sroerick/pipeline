package workspace

import (
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

func Materialize(source, target string, mode Mode) error {
	if err := fsx.RemoveIfExists(target); err != nil {
		return err
	}
	if err := fsx.EnsureDir(filepath.Dir(target)); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
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

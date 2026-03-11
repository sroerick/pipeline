package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func File(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func Dir(path string) (string, int64, error) {
	h := sha256.New()
	var size int64
	var files []string
	if err := filepath.WalkDir(path, func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if current == path {
			return nil
		}
		rel, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	}); err != nil {
		return "", 0, err
	}
	sort.Strings(files)
	for _, rel := range files {
		full := filepath.Join(path, rel)
		info, err := os.Lstat(full)
		if err != nil {
			return "", 0, err
		}
		fmt.Fprintf(h, "%s\x00%s\x00%o\x00", rel, kindForMode(info.Mode()), info.Mode().Perm())
		switch {
		case info.Mode().IsRegular():
			f, err := os.Open(full)
			if err != nil {
				return "", 0, err
			}
			n, err := io.Copy(h, f)
			f.Close()
			if err != nil {
				return "", 0, err
			}
			size += n
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(full)
			if err != nil {
				return "", 0, err
			}
			fmt.Fprintf(h, "%s\x00", target)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func kindForMode(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "dir"
	case mode.IsRegular():
		return "file"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	default:
		return "other"
	}
}

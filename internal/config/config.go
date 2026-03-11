package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"pipe/internal/db"
	"pipe/internal/fsx"
)

const (
	DirName           = ".pipe"
	ConfigRelative    = ".pipe/config.yaml"
	DefaultSpecName   = "pipe.yaml"
	HiddenSpecName    = ".pipe/pipe.yaml"
	DefaultMountMode  = "symlink"
	DefaultExposeMode = "copy"
)

type File struct {
	Version        int    `yaml:"version"`
	ProjectionMode string `yaml:"projection_mode"`
	MountMode      string `yaml:"mount_mode"`
	PublishMode    string `yaml:"publish_mode"`
	ExposeMode     string `yaml:"expose_mode"`
}

type Project struct {
	Root   string
	Config File
}

func Init(root string) (*Project, error) {
	pipeRoot := filepath.Join(root, DirName)
	for _, rel := range []string{
		".",
		"objects",
		"objects/sha256",
		"runs",
		"aliases",
		"mounts",
	} {
		if err := fsx.EnsureDir(filepath.Join(pipeRoot, rel)); err != nil {
			return nil, err
		}
	}
	cfg := File{
		Version:    1,
		MountMode:  DefaultMountMode,
		ExposeMode: DefaultExposeMode,
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return nil, err
	}
	if err := fsx.AtomicWriteFile(filepath.Join(pipeRoot, "config.yaml"), data, 0o644); err != nil {
		return nil, err
	}
	if err := db.Init(filepath.Join(pipeRoot, "db.sqlite")); err != nil {
		return nil, err
	}
	return &Project{Root: root, Config: cfg}, nil
}

func Load(cwd string) (*Project, error) {
	root, err := FindRoot(cwd)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, ConfigRelative))
	if err != nil {
		return nil, err
	}
	var cfg File
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.MountMode == "" {
		if cfg.ProjectionMode != "" {
			cfg.MountMode = cfg.ProjectionMode
		} else {
			cfg.MountMode = DefaultMountMode
		}
	}
	if cfg.PublishMode == "" {
		if cfg.ProjectionMode != "" {
			cfg.PublishMode = cfg.ProjectionMode
		} else if cfg.ExposeMode != "" {
			cfg.PublishMode = cfg.ExposeMode
		} else {
			cfg.PublishMode = DefaultExposeMode
		}
	}
	return &Project{Root: root, Config: cfg}, nil
}

func FindRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, ConfigRelative)); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("project not initialized; run `pipe init`")
		}
		current = parent
	}
}

func DBPath(root string) string {
	return filepath.Join(root, DirName, "db.sqlite")
}

func ManifestPath(root, runID string) string {
	return filepath.Join(root, DirName, "runs", runID, "manifest.json")
}

func StepDir(root, runID, step string) string {
	return filepath.Join(root, DirName, "runs", runID, "steps", step)
}

func SpecPath(root string) string {
	hidden := filepath.Join(root, HiddenSpecName)
	if _, err := os.Stat(hidden); err == nil {
		return hidden
	}
	return filepath.Join(root, DefaultSpecName)
}

func EnsureInitialized(root string) error {
	if _, err := os.Stat(filepath.Join(root, ConfigRelative)); err != nil {
		return fmt.Errorf("project not initialized; run `pipe init`")
	}
	return nil
}

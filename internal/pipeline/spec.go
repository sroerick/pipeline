package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Spec struct {
	Version   int        `yaml:"version"`
	Pipelines []Pipeline `yaml:"pipelines"`
}

type Pipeline struct {
	Name  string `yaml:"name"`
	Steps []Step `yaml:"steps"`
}

type Step struct {
	Name      string            `yaml:"name"`
	Kind      string            `yaml:"kind"`
	Run       string            `yaml:"run"`
	Inputs    []InputRef        `yaml:"inputs"`
	Outputs   []OutputDecl      `yaml:"outputs"`
	Env       map[string]string `yaml:"env"`
	Retention string            `yaml:"retention"`
}

type InputRef struct {
	Source string `yaml:"source"`
	From   string `yaml:"from"`
}

type OutputDecl struct {
	Name    string `yaml:"name"`
	Path    string `yaml:"path"`
	Type    string `yaml:"type"`
	Publish string `yaml:"publish"`
}

func Load(path string) (*Spec, error) {
	return LoadFrom(path, filepath.Dir(path))
}

func LoadFrom(path, projectRoot string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	if err := spec.Validate(projectRoot); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *Spec) Validate(projectRoot string) error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported pipeline spec version %d", s.Version)
	}
	if len(s.Pipelines) == 0 {
		return errors.New("pipeline spec has no pipelines")
	}
	seenPipelines := map[string]struct{}{}
	for _, p := range s.Pipelines {
		if p.Name == "" {
			return errors.New("pipeline name is required")
		}
		if _, ok := seenPipelines[p.Name]; ok {
			return fmt.Errorf("duplicate pipeline %q", p.Name)
		}
		seenPipelines[p.Name] = struct{}{}
		if err := p.Validate(projectRoot); err != nil {
			return fmt.Errorf("pipeline %s: %w", p.Name, err)
		}
	}
	return nil
}

func (p Pipeline) Validate(projectRoot string) error {
	if len(p.Steps) == 0 {
		return errors.New("pipeline has no steps")
	}
	stepNames := map[string]struct{}{}
	outputsByStep := map[string]map[string]struct{}{}
	for _, step := range p.Steps {
		if step.Name == "" {
			return errors.New("step name is required")
		}
		if _, ok := stepNames[step.Name]; ok {
			return fmt.Errorf("duplicate step %q", step.Name)
		}
		stepNames[step.Name] = struct{}{}
		if step.Kind == "" {
			step.Kind = "shell"
		}
		if step.Kind != "shell" && step.Kind != "exec" {
			return fmt.Errorf("step %s has unsupported kind %q", step.Name, step.Kind)
		}
		if strings.TrimSpace(step.Run) == "" {
			return fmt.Errorf("step %s has empty command", step.Name)
		}
		outputNames := map[string]struct{}{}
		if len(step.Outputs) == 0 {
			return fmt.Errorf("step %s declares no outputs", step.Name)
		}
		for _, out := range step.Outputs {
			if out.Name == "" || out.Path == "" || out.Type == "" {
				return fmt.Errorf("step %s has invalid output declaration", step.Name)
			}
			if out.Type != "file" && out.Type != "dir" {
				return fmt.Errorf("step %s output %s has unsupported type %q", step.Name, out.Name, out.Type)
			}
			if out.Publish != "" {
				if filepath.IsAbs(out.Publish) {
					return fmt.Errorf("step %s output %s publish path must be relative", step.Name, out.Name)
				}
				cleaned := filepath.Clean(out.Publish)
				if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
					return fmt.Errorf("step %s output %s publish path escapes project root", step.Name, out.Name)
				}
			}
			if _, ok := outputNames[out.Name]; ok {
				return fmt.Errorf("step %s output %s is duplicated", step.Name, out.Name)
			}
			outputNames[out.Name] = struct{}{}
		}
		outputsByStep[step.Name] = outputNames
	}
	for _, step := range p.Steps {
		for _, in := range step.Inputs {
			switch {
			case in.Source != "":
				if _, err := os.Stat(filepath.Join(projectRoot, in.Source)); err != nil {
					return fmt.Errorf("step %s source input %q: %w", step.Name, in.Source, err)
				}
			case in.From != "":
				prevStep, outName, err := ParseStepOutputRef(in.From)
				if err != nil {
					return fmt.Errorf("step %s input %q: %w", step.Name, in.From, err)
				}
				if _, ok := stepNames[prevStep]; !ok {
					return fmt.Errorf("step %s input references unknown step %q", step.Name, prevStep)
				}
				if _, ok := outputsByStep[prevStep][outName]; !ok {
					return fmt.Errorf("step %s input references unknown output %q on step %s", step.Name, outName, prevStep)
				}
			default:
				return fmt.Errorf("step %s has input missing source/from", step.Name)
			}
		}
	}
	return nil
}

func (s *Spec) ResolvePipeline(name string) (*Pipeline, error) {
	if name == "" {
		if len(s.Pipelines) == 1 {
			return &s.Pipelines[0], nil
		}
		var names []string
		for _, p := range s.Pipelines {
			names = append(names, p.Name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("pipeline name required; available pipelines: %s", strings.Join(names, ", "))
	}
	for i := range s.Pipelines {
		if s.Pipelines[i].Name == name {
			return &s.Pipelines[i], nil
		}
	}
	return nil, fmt.Errorf("unknown pipeline %q", name)
}

func (p Pipeline) StepByName(name string) (*Step, error) {
	for i := range p.Steps {
		if p.Steps[i].Name == name {
			return &p.Steps[i], nil
		}
	}
	return nil, fmt.Errorf("unknown step %q", name)
}

func ParseStepOutputRef(value string) (string, string, error) {
	step, output, ok := strings.Cut(value, "/")
	if !ok || step == "" || output == "" {
		return "", "", fmt.Errorf("invalid step output ref %q", value)
	}
	return step, output, nil
}

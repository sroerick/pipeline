package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Spec struct {
	Version   int        `yaml:"version"`
	Pipelines []Pipeline `yaml:"pipelines"`
}

type Pipeline struct {
	Name    string `yaml:"name"`
	Extends string `yaml:"extends"`
	Steps   []Step `yaml:"steps"`
}

type Step struct {
	Name      string            `yaml:"name"`
	Kind      string            `yaml:"kind"`
	Run       string            `yaml:"run"`
	Inputs    []InputRef        `yaml:"inputs"`
	Outputs   []OutputDecl      `yaml:"outputs"`
	Env       map[string]string `yaml:"env"`
	Retention string            `yaml:"retention"`
	Assert    *AssertSpec       `yaml:"assert"`
}

type InputRef struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
	From   string `yaml:"from"`
	Ref    string `yaml:"ref"`
}

type OutputDecl struct {
	Name    string `yaml:"name"`
	Path    string `yaml:"path"`
	Type    string `yaml:"type"`
	Publish string `yaml:"publish"`
}

type AssertSpec struct {
	SortLines          bool     `yaml:"sort_lines"`
	TrimSpace          bool     `yaml:"trim_space"`
	IgnoreLinePrefixes []string `yaml:"ignore_line_prefixes"`
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
		resolved, err := s.resolvePipeline(p.Name, nil)
		if err != nil {
			return err
		}
		if err := resolved.Validate(projectRoot); err != nil {
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
		kind := step.Kind
		if kind == "" {
			kind = "shell"
		}
		if kind != "shell" && kind != "exec" && kind != "assert" {
			return fmt.Errorf("step %s has unsupported kind %q", step.Name, step.Kind)
		}
		if kind != "assert" && strings.TrimSpace(step.Run) == "" {
			return fmt.Errorf("step %s has empty command", step.Name)
		}
		if kind == "assert" && step.Assert == nil {
			return fmt.Errorf("step %s kind assert requires assert configuration", step.Name)
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
			count := 0
			if in.Source != "" {
				count++
			}
			if in.From != "" {
				count++
			}
			if in.Ref != "" {
				count++
			}
			if count != 1 {
				return fmt.Errorf("step %s input must set exactly one of source/from/ref", step.Name)
			}
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
			case in.Ref != "":
				if _, err := ParseRef(in.Ref); err != nil {
					return fmt.Errorf("step %s input ref %q: %w", step.Name, in.Ref, err)
				}
			default:
				return fmt.Errorf("step %s has input missing source/from/ref", step.Name)
			}
		}
	}
	return nil
}

func (s *Spec) ResolvePipeline(name string) (*Pipeline, error) {
	if name == "" {
		if len(s.Pipelines) == 1 {
			resolved, err := s.resolvePipeline(s.Pipelines[0].Name, nil)
			if err != nil {
				return nil, err
			}
			return &resolved, nil
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
			resolved, err := s.resolvePipeline(name, nil)
			if err != nil {
				return nil, err
			}
			return &resolved, nil
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

func (s *Spec) resolvePipeline(name string, stack []string) (Pipeline, error) {
	index := map[string]Pipeline{}
	for _, p := range s.Pipelines {
		index[p.Name] = p
	}
	return resolvePipelineFromIndex(index, name, stack)
}

func resolvePipelineFromIndex(index map[string]Pipeline, name string, stack []string) (Pipeline, error) {
	p, ok := index[name]
	if !ok {
		return Pipeline{}, fmt.Errorf("unknown pipeline %q", name)
	}
	if slices.Contains(stack, name) {
		return Pipeline{}, fmt.Errorf("pipeline inheritance cycle detected at %q", name)
	}
	if p.Extends == "" {
		return p, nil
	}
	base, err := resolvePipelineFromIndex(index, p.Extends, append(stack, name))
	if err != nil {
		return Pipeline{}, err
	}
	merged := Pipeline{Name: p.Name, Steps: append([]Step{}, base.Steps...)}
	merged.Steps = append(merged.Steps, p.Steps...)
	return merged, nil
}

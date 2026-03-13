package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pakkun/internal/config"
	"pakkun/internal/db"
	"pakkun/internal/fsx"
	"pakkun/internal/graph"
	"pakkun/internal/manifest"
	"pakkun/internal/pipeline"
	"pakkun/internal/resolve"
	"pakkun/internal/runner"
	"pakkun/internal/store"
	"pakkun/internal/workspace"
)

type Engine struct {
	project  *config.Project
	db       *db.DB
	store    *store.Store
	reporter Reporter
}

type RunResult struct {
	RunID     string
	Pipeline  string
	Status    string
	Published []string
	Manifest  manifest.Run
}

type resolvedInput struct {
	manifest manifest.Input
	artifact *db.ArtifactRecord
}

type Reporter interface {
	StepStarted(runID, pipelineName string, step pipeline.Step)
	StepFinished(runID, pipelineName string, step manifest.Step)
}

type noopReporter struct{}

func (noopReporter) StepStarted(string, string, pipeline.Step)  {}
func (noopReporter) StepFinished(string, string, manifest.Step) {}

func New(project *config.Project, database *db.DB) *Engine {
	return &Engine{
		project:  project,
		db:       database,
		store:    store.New(project.Root),
		reporter: noopReporter{},
	}
}

func (e *Engine) WithReporter(reporter Reporter) *Engine {
	e.reporter = reporter
	return e
}

func (e *Engine) RunPipeline(ctx context.Context, pipelineName string) (*RunResult, error) {
	spec, err := pipeline.LoadFrom(config.SpecPath(e.project.Root), e.project.Root)
	if err != nil {
		return nil, err
	}
	def, err := spec.ResolvePipeline(pipelineName)
	if err != nil {
		return nil, err
	}
	ordered, err := graph.TopoSort(*def)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	runID := fmt.Sprintf("%s_%09d", now.Format("20060102_150405"), now.Nanosecond())
	runRoot := filepath.Join(e.project.Root, ".pipe", "runs", runID)
	if err := fsx.EnsureDir(filepath.Join(runRoot, "steps")); err != nil {
		return nil, err
	}
	runRecord := db.RunRecord{
		ID:        runID,
		Pipeline:  def.Name,
		Status:    "running",
		StartedAt: now,
	}
	if err := e.db.CreateRun(runRecord); err != nil {
		return nil, err
	}
	manifestRun := manifest.Run{
		RunID:        runID,
		PipelineName: def.Name,
		StartedAt:    now,
		Status:       "running",
	}
	artifactsByOutput := map[string]db.ArtifactRecord{}
	var runErr error
	for _, step := range ordered {
		e.reporter.StepStarted(runID, def.Name, step)
		stepManifest, produced, err := e.executeStep(ctx, runID, step, artifactsByOutput)
		e.reporter.StepFinished(runID, def.Name, stepManifest)
		manifestRun.Steps = append(manifestRun.Steps, stepManifest)
		for key, value := range produced {
			artifactsByOutput[key] = value
		}
		if err != nil {
			runErr = err
			manifestRun.Status = "failed"
			break
		}
	}
	if runErr == nil {
		manifestRun.Status = "success"
	}
	manifestRun.EndedAt = time.Now().UTC()
	if err := e.db.FinishRun(runID, manifestRun.Status, manifestRun.EndedAt); err != nil {
		return nil, err
	}
	if err := e.db.SetAlias("current", "run:"+runID, manifestRun.EndedAt); err != nil {
		return nil, err
	}
	if err := e.writeManifest(runID, manifestRun); err != nil {
		return nil, err
	}
	var published []string
	if runErr == nil {
		published, err = e.publishDeclaredOutputs(*def, artifactsByOutput)
		if err != nil {
			return nil, err
		}
	}
	result := &RunResult{
		RunID:     runID,
		Pipeline:  def.Name,
		Status:    manifestRun.Status,
		Published: published,
		Manifest:  manifestRun,
	}
	if runErr != nil {
		return result, runErr
	}
	return result, nil
}

func (e *Engine) executeStep(ctx context.Context, runID string, step pipeline.Step, prior map[string]db.ArtifactRecord) (manifest.Step, map[string]db.ArtifactRecord, error) {
	stepDir := config.StepDir(e.project.Root, runID, step.Name)
	workDir := filepath.Join(stepDir, "work")
	if err := fsx.EnsureDir(workDir); err != nil {
		return manifest.Step{}, nil, err
	}
	startedAt := time.Now().UTC()
	resolvedInputs, env, err := e.resolveInputs(step.Inputs, prior)
	if err != nil {
		return manifest.Step{}, nil, err
	}
	env = append(os.Environ(), env...)
	env = append(env,
		"PIPE_PROJECT_ROOT="+e.project.Root,
		"PIPE_RUN_ID="+runID,
		"PIPE_STEP_NAME="+step.Name,
		"PIPE_STEP_OUT="+workDir,
	)
	for key, value := range step.Env {
		env = append(env, key+"="+value)
	}
	var (
		result runner.Result
		runErr error
	)
	if step.Kind == "assert" {
		result, runErr = runAssertStep(ctx, workDir, step, resolvedInputs)
	} else {
		result, runErr = runner.Run(ctx, runner.Request{
			Kind:    step.Kind,
			Command: step.Run,
			Dir:     e.project.Root,
			Env:     env,
		})
	}
	stdoutPath := filepath.Join(stepDir, "stdout.txt")
	stderrPath := filepath.Join(stepDir, "stderr.txt")
	if err := fsx.AtomicWriteFile(stdoutPath, result.Stdout, 0o644); err != nil {
		return manifest.Step{}, nil, err
	}
	if err := fsx.AtomicWriteFile(stderrPath, result.Stderr, 0o644); err != nil {
		return manifest.Step{}, nil, err
	}
	stdoutObj, err := e.store.StoreBytes(result.Stdout)
	if err != nil {
		return manifest.Step{}, nil, err
	}
	stderrObj, err := e.store.StoreBytes(result.Stderr)
	if err != nil {
		return manifest.Step{}, nil, err
	}
	stepManifest := manifest.Step{
		StepName:   step.Name,
		Command:    step.Run,
		Inputs:     inputsToManifest(resolvedInputs),
		ExitCode:   result.ExitCode,
		StdoutRef:  stdoutObj.ObjectRef,
		StderrRef:  stderrObj.ObjectRef,
		StartedAt:  startedAt,
		EndedAt:    time.Now().UTC(),
		WorkingDir: workDir,
		Status:     "success",
	}
	if runErr != nil {
		stepManifest.Status = "failed"
		stepManifest.Error = runErr.Error()
	}
	if err := e.db.UpsertStep(db.StepRecord{
		RunID:           runID,
		StepName:        step.Name,
		Status:          stepManifest.Status,
		Command:         step.Run,
		ExitCode:        result.ExitCode,
		StdoutObjectRef: stdoutObj.ObjectRef,
		StderrObjectRef: stderrObj.ObjectRef,
		StartedAt:       startedAt,
		EndedAt:         stepManifest.EndedAt,
	}); err != nil {
		return manifest.Step{}, nil, err
	}
	produced := map[string]db.ArtifactRecord{}
	for _, out := range step.Outputs {
		resolvedPath, err := fsx.SafeJoin(workDir, out.Path)
		if err != nil {
			return stepManifest, nil, err
		}
		info, err := os.Stat(resolvedPath)
		if err != nil {
			if runErr != nil {
				continue
			}
			stepManifest.Status = "failed"
			stepManifest.Error = fmt.Sprintf("declared output missing after step execution: %s", out.Name)
			_ = e.db.UpsertStep(db.StepRecord{
				RunID:           runID,
				StepName:        step.Name,
				Status:          stepManifest.Status,
				Command:         step.Run,
				ExitCode:        1,
				StdoutObjectRef: stdoutObj.ObjectRef,
				StderrObjectRef: stderrObj.ObjectRef,
				StartedAt:       startedAt,
				EndedAt:         time.Now().UTC(),
			})
			return stepManifest, nil, fmt.Errorf("declared output missing after step execution: %s", out.Name)
		}
		if out.Type == "file" && info.IsDir() {
			return stepManifest, nil, fmt.Errorf("declared file output %s is a directory", out.Name)
		}
		if out.Type == "dir" && !info.IsDir() {
			return stepManifest, nil, fmt.Errorf("declared dir output %s is not a directory", out.Name)
		}
		stored, err := e.store.StoreArtifact(resolvedPath, out.Type)
		if err != nil {
			return stepManifest, nil, err
		}
		stepManifest.Outputs = append(stepManifest.Outputs, manifest.Output{
			Name:       out.Name,
			Path:       out.Path,
			Type:       out.Type,
			ObjectRef:  stored.ObjectRef,
			StoredPath: stored.Path,
			SizeBytes:  stored.SizeBytes,
		})
		artifact := db.ArtifactRecord{
			RunID:        runID,
			StepName:     step.Name,
			OutputName:   out.Name,
			ArtifactType: out.Type,
			ObjectRef:    stored.ObjectRef,
			SizeBytes:    stored.SizeBytes,
			CreatedAt:    time.Now().UTC(),
		}
		artifactID, err := e.db.InsertArtifact(artifact)
		if err != nil {
			return stepManifest, nil, err
		}
		artifact.ID = artifactID
		key := artifactKey(step.Name, out.Name)
		produced[key] = artifact
		for _, input := range resolvedInputs {
			if input.artifact == nil {
				continue
			}
			if err := e.db.InsertProvenanceEdge(input.artifact.ID, artifactID, step.Name); err != nil {
				return stepManifest, nil, err
			}
		}
	}
	if runErr != nil {
		return stepManifest, produced, fmt.Errorf("step %s failed: %w", step.Name, runErr)
	}
	return stepManifest, produced, nil
}

func (e *Engine) resolveInputs(inputs []pipeline.InputRef, prior map[string]db.ArtifactRecord) ([]resolvedInput, []string, error) {
	var resolved []resolvedInput
	var env []string
	for _, input := range inputs {
		switch {
		case input.Source != "":
			fullPath, err := fsx.SafeJoin(e.project.Root, input.Source)
			if err != nil {
				return nil, nil, err
			}
			if _, err := os.Stat(fullPath); err != nil {
				return nil, nil, err
			}
			resolved = append(resolved, resolvedInput{
				manifest: manifest.Input{
					Kind:         "source",
					Source:       input.Source,
					ResolvedPath: fullPath,
				},
			})
		case input.From != "":
			stepName, outputName, err := pipeline.ParseStepOutputRef(input.From)
			if err != nil {
				return nil, nil, err
			}
			artifact, ok := prior[artifactKey(stepName, outputName)]
			if !ok {
				return nil, nil, fmt.Errorf("missing prior artifact %s", input.From)
			}
			storedPath, err := e.store.Resolve(artifact.ObjectRef)
			if err != nil {
				return nil, nil, err
			}
			env = append(env, "PIPE_INPUT_"+sanitizeEnvName(inputEnvName(input.Name, outputName))+"="+storedPath)
			resolved = append(resolved, resolvedInput{
				manifest: manifest.Input{
					Kind:         "from",
					Ref:          input.From,
					ResolvedPath: storedPath,
					ObjectRef:    artifact.ObjectRef,
				},
				artifact: &artifact,
			})
		case input.Ref != "":
			ref, err := resolve.Alias(e.db, input.Ref)
			if err != nil {
				return nil, nil, err
			}
			resolvedRef, err := resolve.Ref(e.project.Root, e.db, ref)
			if err != nil {
				return nil, nil, err
			}
			if resolvedRef.Artifact == nil {
				return nil, nil, fmt.Errorf("input ref %s did not resolve to an artifact", input.Ref)
			}
			name := inputEnvName(input.Name, resolvedRef.Artifact.OutputName)
			env = append(env, "PIPE_INPUT_"+sanitizeEnvName(name)+"="+resolvedRef.StoredPath)
			artifact := *resolvedRef.Artifact
			resolved = append(resolved, resolvedInput{
				manifest: manifest.Input{
					Kind:         "ref",
					Ref:          input.Ref,
					ResolvedPath: resolvedRef.StoredPath,
					ObjectRef:    artifact.ObjectRef,
				},
				artifact: &artifact,
			})
		default:
			return nil, nil, errors.New("input missing source/from/ref")
		}
	}
	return resolved, env, nil
}

func (e *Engine) writeManifest(runID string, m manifest.Run) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsx.AtomicWriteFile(config.ManifestPath(e.project.Root, runID), data, 0o644)
}

func (e *Engine) publishDeclaredOutputs(def pipeline.Pipeline, artifacts map[string]db.ArtifactRecord) ([]string, error) {
	var published []string
	for _, step := range def.Steps {
		for _, out := range step.Outputs {
			if out.Publish == "" {
				continue
			}
			artifact, ok := artifacts[artifactKey(step.Name, out.Name)]
			if !ok {
				return nil, fmt.Errorf("missing artifact for publish target %s:%s/%s", def.Name, step.Name, out.Name)
			}
			source, err := e.store.Resolve(artifact.ObjectRef)
			if err != nil {
				return nil, err
			}
			target, err := fsx.SafeJoin(e.project.Root, out.Publish)
			if err != nil {
				return nil, err
			}
			if err := workspace.Materialize(source, target, workspace.Mode(e.project.Config.PublishMode)); err != nil {
				return nil, err
			}
			published = append(published, target)
		}
	}
	return published, nil
}

func inputsToManifest(values []resolvedInput) []manifest.Input {
	var out []manifest.Input
	for _, value := range values {
		out = append(out, value.manifest)
	}
	return out
}

func artifactKey(step, output string) string {
	return step + "/" + output
}

func sanitizeEnvName(value string) string {
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, ".", "_")
	value = strings.ReplaceAll(value, "/", "_")
	return value
}

func inputEnvName(explicit, fallback string) string {
	if explicit != "" {
		return explicit
	}
	return fallback
}

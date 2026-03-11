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

	"pipe/internal/config"
	"pipe/internal/db"
	"pipe/internal/fsx"
	"pipe/internal/graph"
	"pipe/internal/manifest"
	"pipe/internal/pipeline"
	"pipe/internal/runner"
	"pipe/internal/store"
)

type Engine struct {
	project *config.Project
	db      *db.DB
	store   *store.Store
}

type RunResult struct {
	RunID    string
	Pipeline string
	Status   string
	Manifest manifest.Run
}

type resolvedInput struct {
	manifest manifest.Input
	artifact *db.ArtifactRecord
}

func New(project *config.Project, database *db.DB) *Engine {
	return &Engine{
		project: project,
		db:      database,
		store:   store.New(project.Root),
	}
}

func (e *Engine) RunPipeline(ctx context.Context, pipelineName string) (*RunResult, error) {
	spec, err := pipeline.Load(config.SpecPath(e.project.Root))
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
	runID := now.Format("20060102_150405_000000000")
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
		stepManifest, produced, err := e.executeStep(ctx, runID, step, artifactsByOutput)
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
	result := &RunResult{
		RunID:    runID,
		Pipeline: def.Name,
		Status:   manifestRun.Status,
		Manifest: manifestRun,
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
	result, runErr := runner.Run(ctx, runner.Request{
		Kind:    step.Kind,
		Command: step.Run,
		Dir:     e.project.Root,
		Env:     env,
	})
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
	if runErr != nil {
		return stepManifest, nil, fmt.Errorf("step %s failed: %w", step.Name, runErr)
	}
	produced := map[string]db.ArtifactRecord{}
	for _, out := range step.Outputs {
		resolvedPath, err := fsx.SafeJoin(workDir, out.Path)
		if err != nil {
			return stepManifest, nil, err
		}
		info, err := os.Stat(resolvedPath)
		if err != nil {
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
			env = append(env, "PIPE_INPUT_"+sanitizeEnvName(outputName)+"="+storedPath)
			resolved = append(resolved, resolvedInput{
				manifest: manifest.Input{
					Kind:         "from",
					Ref:          input.From,
					ResolvedPath: storedPath,
					ObjectRef:    artifact.ObjectRef,
				},
				artifact: &artifact,
			})
		default:
			return nil, nil, errors.New("input missing source/from")
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
	return value
}

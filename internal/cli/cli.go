package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pipe/internal/config"
	"pipe/internal/db"
	"pipe/internal/engine"
	"pipe/internal/fsx"
	"pipe/internal/pipeline"
	"pipe/internal/store"
	"pipe/internal/ui"
	"pipe/internal/workspace"
)

func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "init":
		return cmdInit()
	case "run":
		pipelineName := ""
		if len(args) > 1 {
			pipelineName = args[1]
		}
		return cmdRun(ctx, pipelineName)
	case "stages":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		return cmdStages(target)
	case "status":
		return cmdStatus()
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: pipe show <ref>")
		}
		return cmdShow(args[1])
	case "mount":
		if len(args) != 3 {
			return fmt.Errorf("usage: pipe mount <ref> <dir>")
		}
		return cmdMount(args[1], args[2])
	case "expose":
		if len(args) != 3 {
			return fmt.Errorf("usage: pipe expose <ref> <path>")
		}
		return cmdExpose(args[1], args[2])
	case "log":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		return cmdLog(target)
	case "provenance":
		if len(args) != 2 {
			return fmt.Errorf("usage: pipe provenance <ref>")
		}
		return cmdProvenance(args[1])
	default:
		return usage()
	}
}

func cmdInit() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(cwd, config.ConfigRelative)); err == nil {
		fmt.Println("already initialized:", cwd)
		return nil
	}
	project, err := config.Init(cwd)
	if err != nil {
		return err
	}
	fmt.Println("initialized pipe project in", filepath.Join(project.Root, ".pipe"))
	return nil
}

func cmdRun(ctx context.Context, pipelineName string) error {
	project, database, err := openProject()
	if err != nil {
		return err
	}
	defer database.Close()
	result, runErr := engine.New(project, database).RunPipeline(ctx, pipelineName)
	if result == nil {
		return runErr
	}
	fmt.Println(ui.Heading("Run"))
	fmt.Println(ui.KV("run", result.RunID))
	fmt.Println(ui.KV("pipeline", result.Pipeline))
	fmt.Println(ui.KV("status", result.Status))
	if runErr != nil {
		return runErr
	}
	return nil
}

func cmdStages(target string) error {
	project, database, err := openProject()
	if err != nil {
		return err
	}
	defer database.Close()
	fmt.Println(ui.Heading("Stages"))
	if strings.HasPrefix(target, "run:") {
		runID := strings.TrimPrefix(target, "run:")
		return printRunStages(database, runID)
	}
	spec, err := pipeline.Load(config.SpecPath(project.Root))
	if err != nil {
		return err
	}
	def, err := spec.ResolvePipeline(target)
	if err != nil {
		return err
	}
	for _, step := range def.Steps {
		fmt.Printf("%s (%s)\n", step.Name, step.Kind)
		for _, out := range step.Outputs {
			fmt.Printf("  - %s -> %s [%s]\n", out.Name, out.Path, out.Type)
		}
	}
	return nil
}

func cmdStatus() error {
	project, database, err := openProject()
	if err != nil {
		return err
	}
	defer database.Close()
	spec, specErr := pipeline.Load(config.SpecPath(project.Root))
	runs, _ := database.ListRuns(5, "")
	aliases, _ := database.ListAliases()
	failedSteps, _ := database.ListFailedSteps(5)
	fmt.Println(ui.Heading("Status"))
	fmt.Println(ui.KV("project", project.Root))
	if specErr == nil {
		var names []string
		for _, p := range spec.Pipelines {
			names = append(names, p.Name)
		}
		fmt.Println(ui.KV("pipelines", strings.Join(names, ", ")))
	} else {
		fmt.Println(ui.KV("pipelines", "missing pipe.yaml"))
	}
	if len(runs) == 0 {
		fmt.Println(ui.KV("latest runs", "none"))
	} else {
		fmt.Println("latest runs:")
		for _, run := range runs {
			fmt.Printf("  %s  %s  %s\n", run.ID, run.Pipeline, run.Status)
		}
	}
	if len(failedSteps) > 0 {
		fmt.Println("failed steps:")
		for _, step := range failedSteps {
			fmt.Printf("  %s  %s  %s\n", step.RunID, step.StepName, step.Status)
		}
	}
	if len(aliases) > 0 {
		fmt.Println("aliases:")
		for _, alias := range aliases {
			fmt.Printf("  %s -> %s\n", alias.Name, alias.TargetRef)
		}
	}
	return nil
}

func cmdShow(rawRef string) error {
	project, database, err := openProject()
	if err != nil {
		return err
	}
	defer database.Close()
	ref, err := resolveAlias(database, rawRef)
	if err != nil {
		return err
	}
	resolved, err := resolveRef(project, database, ref)
	if err != nil {
		return err
	}
	fmt.Println(ui.Heading("Show"))
	switch {
	case resolved.Artifact != nil:
		fmt.Println(ui.KV("ref", rawRef))
		fmt.Println(ui.KV("run", resolved.Run.ID))
		fmt.Println(ui.KV("pipeline", resolved.Run.Pipeline))
		fmt.Println(ui.KV("step", resolved.Step.StepName))
		fmt.Println(ui.KV("output", resolved.Artifact.OutputName))
		fmt.Println(ui.KV("type", resolved.Artifact.ArtifactType))
		fmt.Println(ui.KV("object", resolved.Artifact.ObjectRef))
		fmt.Println(ui.KV("path", resolved.StoredPath))
	case resolved.Step != nil:
		fmt.Println(ui.KV("ref", rawRef))
		fmt.Println(ui.KV("run", resolved.Run.ID))
		fmt.Println(ui.KV("pipeline", resolved.Run.Pipeline))
		fmt.Println(ui.KV("step", resolved.Step.StepName))
		fmt.Println(ui.KV("status", resolved.Step.Status))
		artifacts, err := database.ListArtifacts(resolved.Run.ID, resolved.Step.StepName)
		if err != nil {
			return err
		}
		for _, artifact := range artifacts {
			path, _ := store.New(project.Root).Resolve(artifact.ObjectRef)
			fmt.Printf("  %s -> %s (%s)\n", artifact.OutputName, path, artifact.ArtifactType)
		}
	case resolved.Run != nil:
		fmt.Println(ui.KV("run", resolved.Run.ID))
		fmt.Println(ui.KV("pipeline", resolved.Run.Pipeline))
		fmt.Println(ui.KV("status", resolved.Run.Status))
		fmt.Println(ui.KV("started", ui.Time(resolved.Run.StartedAt)))
		fmt.Println(ui.KV("ended", ui.Time(resolved.Run.EndedAt)))
	default:
		return errors.New("nothing resolved")
	}
	return nil
}

func cmdMount(rawRef, dir string) error {
	project, database, err := openProject()
	if err != nil {
		return err
	}
	defer database.Close()
	ref, err := resolveAlias(database, rawRef)
	if err != nil {
		return err
	}
	resolved, err := resolveRef(project, database, ref)
	if err != nil {
		return err
	}
	targetDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if resolved.Run != nil && resolved.Step == nil {
		return fmt.Errorf("mount expects a stage or artifact ref, not a run ref")
	}
	if err := fsx.RemoveIfExists(targetDir); err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	mode := workspace.Mode(project.Config.ProjectionMode)
	if resolved.Artifact != nil {
		target := filepath.Join(targetDir, resolved.Artifact.OutputName)
		if err := workspace.Materialize(resolved.StoredPath, target, mode); err != nil {
			return err
		}
		fmt.Println(target)
		return nil
	}
	artifacts, err := database.ListArtifacts(resolved.Run.ID, resolved.Step.StepName)
	if err != nil {
		return err
	}
	objStore := store.New(project.Root)
	for _, artifact := range artifacts {
		source, err := objStore.Resolve(artifact.ObjectRef)
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, artifact.OutputName)
		if err := workspace.Materialize(source, target, mode); err != nil {
			return err
		}
	}
	fmt.Println(targetDir)
	return nil
}

func cmdExpose(rawRef, path string) error {
	project, database, err := openProject()
	if err != nil {
		return err
	}
	defer database.Close()
	ref, err := resolveAlias(database, rawRef)
	if err != nil {
		return err
	}
	resolved, err := resolveRef(project, database, ref)
	if err != nil {
		return err
	}
	if resolved.Artifact == nil {
		return fmt.Errorf("expose requires an artifact ref")
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := workspace.Materialize(resolved.StoredPath, target, workspace.Mode(project.Config.ProjectionMode)); err != nil {
		return err
	}
	fmt.Println(target)
	return nil
}

func cmdLog(target string) error {
	_, database, err := openProject()
	if err != nil {
		return err
	}
	defer database.Close()
	fmt.Println(ui.Heading("Log"))
	if strings.HasPrefix(target, "run:") {
		runID := strings.TrimPrefix(target, "run:")
		run, err := database.GetRun(runID)
		if err != nil {
			return err
		}
		fmt.Printf("%s  %s  %s\n", run.ID, run.Pipeline, run.Status)
		return printRunStages(database, runID)
	}
	runs, err := database.ListRuns(10, target)
	if err != nil {
		return err
	}
	for _, run := range runs {
		fmt.Printf("%s  %s  %s  %s\n", run.ID, run.Pipeline, run.Status, ui.Time(run.StartedAt))
	}
	return nil
}

func cmdProvenance(rawRef string) error {
	project, database, err := openProject()
	if err != nil {
		return err
	}
	defer database.Close()
	ref, err := resolveAlias(database, rawRef)
	if err != nil {
		return err
	}
	resolved, err := resolveRef(project, database, ref)
	if err != nil {
		return err
	}
	fmt.Println(ui.Heading("Provenance"))
	if resolved.Artifact != nil {
		return printArtifactProvenance(database, resolved.Artifact)
	}
	if resolved.Step != nil {
		fmt.Println(ui.KV("run", resolved.Run.ID))
		fmt.Println(ui.KV("step", resolved.Step.StepName))
		artifacts, err := database.ListArtifacts(resolved.Run.ID, resolved.Step.StepName)
		if err != nil {
			return err
		}
		for _, artifact := range artifacts {
			fmt.Printf("%s:\n", artifact.OutputName)
			if err := printArtifactProvenance(database, &artifact); err != nil {
				return err
			}
		}
		return nil
	}
	fmt.Println(ui.KV("run", resolved.Run.ID))
	fmt.Println(ui.KV("pipeline", resolved.Run.Pipeline))
	return nil
}

func openProject() (*config.Project, *db.DB, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	project, err := config.Load(cwd)
	if err != nil {
		return nil, nil, err
	}
	database, err := db.Open(config.DBPath(project.Root))
	if err != nil {
		return nil, nil, err
	}
	return project, database, nil
}

func printRunStages(database *db.DB, runID string) error {
	run, err := database.GetRun(runID)
	if err != nil {
		return err
	}
	fmt.Printf("%s (%s)\n", run.ID, run.Status)
	steps, err := database.ListSteps(runID)
	if err != nil {
		return err
	}
	for _, step := range steps {
		fmt.Printf("  %s  %s\n", step.StepName, step.Status)
		artifacts, err := database.ListArtifacts(runID, step.StepName)
		if err != nil {
			return err
		}
		for _, artifact := range artifacts {
			fmt.Printf("    - %s [%s]\n", artifact.OutputName, artifact.ArtifactType)
		}
	}
	return nil
}

type resolvedRef struct {
	Run        *db.RunRecord
	Step       *db.StepRecord
	Artifact   *db.ArtifactRecord
	StoredPath string
}

func resolveAlias(database *db.DB, raw string) (pipeline.Ref, error) {
	ref, err := pipeline.ParseRef(raw)
	if err != nil {
		if strings.HasPrefix(raw, "run:") && strings.Count(raw, ":") == 1 {
			return pipeline.Ref{Kind: pipeline.RefRun, RunID: strings.TrimPrefix(raw, "run:")}, nil
		}
		return pipeline.Ref{}, err
	}
	if ref.Kind != pipeline.RefAlias {
		return ref, nil
	}
	alias, err := database.GetAlias(ref.Alias)
	if err != nil {
		return pipeline.Ref{}, err
	}
	return pipeline.ParseRef(alias.TargetRef)
}

func resolveRef(project *config.Project, database *db.DB, ref pipeline.Ref) (resolvedRef, error) {
	objStore := store.New(project.Root)
	switch ref.Kind {
	case pipeline.RefRun:
		run, err := database.GetRun(ref.RunID)
		if err != nil {
			return resolvedRef{}, err
		}
		result := resolvedRef{Run: &run}
		if ref.Step == "" {
			return result, nil
		}
		step, err := database.GetStep(ref.RunID, ref.Step)
		if err != nil {
			return resolvedRef{}, err
		}
		result.Step = &step
		if ref.Output == "" {
			return result, nil
		}
		artifact, err := database.GetArtifact(ref.RunID, ref.Step, ref.Output)
		if err != nil {
			return resolvedRef{}, err
		}
		path, err := objStore.Resolve(artifact.ObjectRef)
		if err != nil {
			return resolvedRef{}, err
		}
		result.Artifact = &artifact
		result.StoredPath = path
		return result, nil
	case pipeline.RefPipeline:
		run, err := database.GetLatestSuccessfulRunForPipeline(ref.Pipeline)
		if err != nil {
			if db.IsNotFound(err) {
				return resolvedRef{}, fmt.Errorf("pipeline %s has no successful runs yet", ref.Pipeline)
			}
			return resolvedRef{}, err
		}
		return resolveRef(project, database, pipeline.Ref{
			Kind:   pipeline.RefRun,
			RunID:  run.ID,
			Step:   ref.Step,
			Output: ref.Output,
		})
	default:
		return resolvedRef{}, fmt.Errorf("unsupported ref %s", ref.String())
	}
}

func printArtifactProvenance(database *db.DB, artifact *db.ArtifactRecord) error {
	fmt.Printf("  run: %s\n", artifact.RunID)
	fmt.Printf("  step: %s\n", artifact.StepName)
	fmt.Printf("  output: %s\n", artifact.OutputName)
	fmt.Printf("  object: %s\n", artifact.ObjectRef)
	edges, err := database.ListIncomingProvenance(artifact.ID)
	if err != nil {
		return err
	}
	if len(edges) == 0 {
		fmt.Println("  inputs: none")
		return nil
	}
	fmt.Println("  inputs:")
	for _, edge := range edges {
		fmt.Printf("    %s:%s/%s\n", edge.From.RunID, edge.From.StepName, edge.From.OutputName)
	}
	return nil
}

func usage() error {
	return fmt.Errorf("usage: pipe <init|run|stages|status|show|mount|expose|log|provenance>")
}

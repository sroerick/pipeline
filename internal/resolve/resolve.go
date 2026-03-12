package resolve

import (
	"fmt"
	"strings"

	"pipe/internal/db"
	"pipe/internal/pipeline"
	"pipe/internal/store"
)

type RefResult struct {
	Run        *db.RunRecord
	Step       *db.StepRecord
	Artifact   *db.ArtifactRecord
	StoredPath string
}

func Alias(database *db.DB, raw string) (pipeline.Ref, error) {
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

func Ref(projectRoot string, database *db.DB, ref pipeline.Ref) (RefResult, error) {
	objStore := store.New(projectRoot)
	switch ref.Kind {
	case pipeline.RefRun:
		run, err := database.GetRun(ref.RunID)
		if err != nil {
			return RefResult{}, err
		}
		result := RefResult{Run: &run}
		if ref.Step == "" {
			return result, nil
		}
		step, err := database.GetStep(ref.RunID, ref.Step)
		if err != nil {
			return RefResult{}, err
		}
		result.Step = &step
		if ref.Output == "" {
			return result, nil
		}
		artifact, err := database.GetArtifact(ref.RunID, ref.Step, ref.Output)
		if err != nil {
			return RefResult{}, err
		}
		path, err := objStore.Resolve(artifact.ObjectRef)
		if err != nil {
			return RefResult{}, err
		}
		result.Artifact = &artifact
		result.StoredPath = path
		return result, nil
	case pipeline.RefPipeline:
		run, err := database.GetLatestSuccessfulRunForPipeline(ref.Pipeline)
		if err != nil {
			if db.IsNotFound(err) {
				return RefResult{}, fmt.Errorf("pipeline %s has no successful runs yet", ref.Pipeline)
			}
			return RefResult{}, err
		}
		return Ref(projectRoot, database, pipeline.Ref{
			Kind:   pipeline.RefRun,
			RunID:  run.ID,
			Step:   ref.Step,
			Output: ref.Output,
		})
	default:
		return RefResult{}, fmt.Errorf("unsupported ref %s", ref.String())
	}
}

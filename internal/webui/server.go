package webui

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"pakkun/internal/config"
	"pakkun/internal/db"
	"pakkun/internal/engine"
	"pakkun/internal/fsx"
	"pakkun/internal/pipeline"
	"pakkun/internal/resolve"
	"pakkun/internal/store"
	"pakkun/internal/workspace"
)

const (
	defaultAddr       = "127.0.0.1:0"
	maxPreviewBytes   = 64 * 1024
	maxLogPreviewByte = 64 * 1024
)

//go:embed assets/index.html assets/app.css assets/app.js
var assets embed.FS

type App struct {
	ctx      context.Context
	project  *config.Project
	database *db.DB
	store    *store.Store
}

type statusResponse struct {
	ProjectRoot string            `json:"project_root"`
	SpecPath    string            `json:"spec_path"`
	Pipelines   []pipelineSummary `json:"pipelines"`
	LatestRuns  []runSummary      `json:"latest_runs"`
	FailedSteps []stepSummary     `json:"failed_steps"`
	Aliases     []aliasSummary    `json:"aliases"`
}

type pipelineSummary struct {
	Name      string      `json:"name"`
	StepCount int         `json:"step_count"`
	LatestRun *runSummary `json:"latest_run,omitempty"`
	Aliases   []string    `json:"aliases,omitempty"`
}

type pipelineDetail struct {
	Name      string         `json:"name"`
	StepCount int            `json:"step_count"`
	LatestRun *runSummary    `json:"latest_run,omitempty"`
	Steps     []stepSpecView `json:"steps"`
}

type runSummary struct {
	ID         string    `json:"id"`
	Pipeline   string    `json:"pipeline"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	DurationMS int64     `json:"duration_ms"`
}

type stepSummary struct {
	RunID       string         `json:"run_id"`
	StepName    string         `json:"step_name"`
	Status      string         `json:"status"`
	Command     string         `json:"command"`
	ExitCode    int            `json:"exit_code"`
	StartedAt   time.Time      `json:"started_at"`
	EndedAt     time.Time      `json:"ended_at"`
	DurationMS  int64          `json:"duration_ms"`
	StdoutRef   string         `json:"stdout_ref,omitempty"`
	StderrRef   string         `json:"stderr_ref,omitempty"`
	Stdout      string         `json:"stdout,omitempty"`
	StdoutShort bool           `json:"stdout_truncated,omitempty"`
	Stderr      string         `json:"stderr,omitempty"`
	StderrShort bool           `json:"stderr_truncated,omitempty"`
	Artifacts   []artifactView `json:"artifacts,omitempty"`
}

type artifactView struct {
	Ref          string    `json:"ref"`
	RunID        string    `json:"run_id"`
	StepName     string    `json:"step_name"`
	OutputName   string    `json:"output_name"`
	ArtifactType string    `json:"artifact_type"`
	ObjectRef    string    `json:"object_ref"`
	StoredPath   string    `json:"stored_path"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
	Preview      string    `json:"preview,omitempty"`
	PreviewShort bool      `json:"preview_truncated,omitempty"`
}

type runDetail struct {
	Run      runSummary    `json:"run"`
	Steps    []stepSummary `json:"steps"`
	Manifest manifestView  `json:"manifest"`
}

type manifestView struct {
	Path string `json:"path"`
	Raw  string `json:"raw"`
}

type aliasSummary struct {
	Name      string    `json:"name"`
	TargetRef string    `json:"target_ref"`
	UpdatedAt time.Time `json:"updated_at"`
}

type stepSpecView struct {
	Name         string           `json:"name"`
	Kind         string           `json:"kind"`
	Command      string           `json:"command"`
	Dependencies []string         `json:"dependencies"`
	Inputs       []inputSpecView  `json:"inputs"`
	Outputs      []outputSpecView `json:"outputs"`
}

type inputSpecView struct {
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
	From   string `json:"from,omitempty"`
	Ref    string `json:"ref,omitempty"`
}

type outputSpecView struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Publish string `json:"publish,omitempty"`
}

type provenanceResponse struct {
	Ref       string            `json:"ref"`
	Kind      string            `json:"kind"`
	Run       *runSummary       `json:"run,omitempty"`
	Step      *stepSummary      `json:"step,omitempty"`
	Artifact  *artifactView     `json:"artifact,omitempty"`
	Artifacts []artifactLineage `json:"artifacts,omitempty"`
}

type artifactLineage struct {
	Artifact artifactView     `json:"artifact"`
	Inputs   []provenanceEdge `json:"inputs"`
}

type provenanceEdge struct {
	ViaStep string       `json:"via_step"`
	From    artifactView `json:"from"`
	FromRef string       `json:"from_ref"`
}

type runRequest struct {
	Pipeline string `json:"pipeline"`
}

type runResponse struct {
	Run   *runSummary `json:"run,omitempty"`
	Error string      `json:"error,omitempty"`
}

type publishRequest struct {
	Ref  string `json:"ref"`
	Path string `json:"path"`
}

type publishResponse struct {
	Path string `json:"path"`
}

func Serve(ctx context.Context, project *config.Project, database *db.DB, addr string, stdout io.Writer) error {
	if addr == "" {
		addr = defaultAddr
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: Handler(ctx, project, database)}
	urlText := listenerURL(listener.Addr())
	fmt.Fprintf(stdout, "pakkun ui available at %s\n", urlText)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		err = <-errCh
	case err = <-errCh:
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func Handler(ctx context.Context, project *config.Project, database *db.DB) http.Handler {
	if loaded, err := config.Load(project.Root); err == nil {
		project = loaded
	}
	app := &App{
		ctx:      ctx,
		project:  project,
		database: database,
		store:    store.New(project.Root),
	}
	return app.routes()
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/app.css", a.handleAsset("assets/app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/app.js", a.handleAsset("assets/app.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/pipelines", a.handlePipelines)
	mux.HandleFunc("/api/pipelines/", a.handlePipeline)
	mux.HandleFunc("/api/runs", a.handleRuns)
	mux.HandleFunc("/api/runs/", a.handleRun)
	mux.HandleFunc("/api/artifact", a.handleArtifact)
	mux.HandleFunc("/api/provenance", a.handleProvenance)
	mux.HandleFunc("/api/download", a.handleDownload)
	mux.HandleFunc("/api/run", a.handleRunAction)
	mux.HandleFunc("/api/publish", a.handlePublish)
	return a.withJSONErrors(mux)
}

func (a *App) withJSONErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (a *App) handleAsset(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := assets.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(data)
	}
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	spec, err := pipeline.LoadFrom(config.SpecPath(a.project.Root), a.project.Root)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	runs, err := a.database.ListRuns(10, "")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	failedSteps, err := a.database.ListFailedSteps(10)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	aliases, err := a.database.ListAliases()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	aliasByPipeline := map[string][]string{}
	for _, alias := range aliases {
		ref, err := pipeline.ParseRef(alias.TargetRef)
		if err != nil {
			continue
		}
		switch ref.Kind {
		case pipeline.RefPipeline:
			aliasByPipeline[ref.Pipeline] = append(aliasByPipeline[ref.Pipeline], alias.Name)
		case pipeline.RefRun:
			run, err := a.database.GetRun(ref.RunID)
			if err == nil {
				aliasByPipeline[run.Pipeline] = append(aliasByPipeline[run.Pipeline], alias.Name)
			}
		}
	}
	response := statusResponse{
		ProjectRoot: a.project.Root,
		SpecPath:    config.SpecPath(a.project.Root),
		LatestRuns:  summarizeRuns(runs),
		FailedSteps: summarizeSteps(failedSteps),
		Aliases:     summarizeAliases(aliases),
	}
	for _, candidate := range spec.Pipelines {
		resolved, err := spec.ResolvePipeline(candidate.Name)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		item := pipelineSummary{
			Name:      candidate.Name,
			StepCount: len(resolved.Steps),
			Aliases:   aliasByPipeline[candidate.Name],
		}
		latest, err := a.database.ListRuns(1, candidate.Name)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		if len(latest) > 0 {
			run := summarizeRun(latest[0])
			item.LatestRun = &run
		}
		response.Pipelines = append(response.Pipelines, item)
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handlePipelines(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/pipelines" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	spec, err := pipeline.LoadFrom(config.SpecPath(a.project.Root), a.project.Root)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	var items []pipelineSummary
	for _, candidate := range spec.Pipelines {
		resolved, err := spec.ResolvePipeline(candidate.Name)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		item := pipelineSummary{
			Name:      candidate.Name,
			StepCount: len(resolved.Steps),
		}
		latest, err := a.database.ListRuns(1, candidate.Name)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		if len(latest) > 0 {
			run := summarizeRun(latest[0])
			item.LatestRun = &run
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *App) handlePipeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/pipelines/")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	pipelineName, err := url.PathUnescape(name)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	spec, err := pipeline.LoadFrom(config.SpecPath(a.project.Root), a.project.Root)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	def, err := spec.ResolvePipeline(pipelineName)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err)
		return
	}
	response := pipelineDetail{
		Name:      def.Name,
		StepCount: len(def.Steps),
		Steps:     summarizeStepSpecs(def.Steps),
	}
	runs, err := a.database.ListRuns(1, pipelineName)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	if len(runs) > 0 {
		run := summarizeRun(runs[0])
		response.LatestRun = &run
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/runs" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err)
			return
		}
		limit = value
	}
	runs, err := a.database.ListRuns(limit, r.URL.Query().Get("pipeline"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summarizeRuns(runs))
}

func (a *App) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	runID := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	if runID == "" {
		http.NotFound(w, r)
		return
	}
	runID, err := url.PathUnescape(runID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	run, err := a.database.GetRun(runID)
	if err != nil {
		status := http.StatusInternalServerError
		if db.IsNotFound(err) {
			status = http.StatusNotFound
		}
		writeAPIError(w, status, err)
		return
	}
	steps, err := a.database.ListSteps(runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	var details []stepSummary
	for _, step := range steps {
		item, err := a.stepDetail(run.Pipeline, step)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		details = append(details, item)
	}
	manifestPath := config.ManifestPath(a.project.Root, runID)
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runDetail{
		Run:   summarizeRun(run),
		Steps: details,
		Manifest: manifestView{
			Path: manifestPath,
			Raw:  string(manifestBytes),
		},
	})
}

func (a *App) handleArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("missing ref"))
		return
	}
	resolved, rawRef, err := a.resolveReference(ref)
	if err != nil {
		status := http.StatusInternalServerError
		if db.IsNotFound(err) {
			status = http.StatusNotFound
		}
		writeAPIError(w, status, err)
		return
	}
	if resolved.Artifact == nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("ref %s did not resolve to an artifact", ref))
		return
	}
	item, err := a.artifactDetail(rawRef, *resolved.Artifact, resolved.StoredPath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) handleProvenance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("missing ref"))
		return
	}
	resolved, rawRef, err := a.resolveReference(ref)
	if err != nil {
		status := http.StatusInternalServerError
		if db.IsNotFound(err) {
			status = http.StatusNotFound
		}
		writeAPIError(w, status, err)
		return
	}
	response := provenanceResponse{Ref: rawRef}
	switch {
	case resolved.Artifact != nil:
		response.Kind = "artifact"
		artifact, err := a.artifactDetail(rawRef, *resolved.Artifact, resolved.StoredPath)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		response.Artifact = &artifact
		item, err := a.lineageForArtifact(rawRef, *resolved.Artifact, resolved.StoredPath)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		response.Artifacts = []artifactLineage{item}
	case resolved.Step != nil:
		response.Kind = "step"
		run := summarizeRun(*resolved.Run)
		response.Run = &run
		step, err := a.stepDetail(resolved.Run.Pipeline, *resolved.Step)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		response.Step = &step
		for _, artifact := range step.Artifacts {
			record, err := a.database.GetArtifact(resolved.Run.ID, resolved.Step.StepName, artifact.OutputName)
			if err != nil {
				writeAPIError(w, http.StatusInternalServerError, err)
				return
			}
			item, err := a.lineageForArtifact(artifact.Ref, record, artifact.StoredPath)
			if err != nil {
				writeAPIError(w, http.StatusInternalServerError, err)
				return
			}
			response.Artifacts = append(response.Artifacts, item)
		}
	default:
		response.Kind = "run"
		run := summarizeRun(*resolved.Run)
		response.Run = &run
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("missing ref"))
		return
	}
	resolved, _, err := a.resolveReference(ref)
	if err != nil {
		status := http.StatusInternalServerError
		if db.IsNotFound(err) {
			status = http.StatusNotFound
		}
		writeAPIError(w, status, err)
		return
	}
	if resolved.Artifact == nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("ref %s did not resolve to an artifact", ref))
		return
	}
	if resolved.Artifact.ArtifactType != "file" {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("download only supports file artifacts"))
		return
	}
	filename := filepath.Base(resolved.StoredPath)
	if resolved.Artifact.OutputName != "" {
		filename = resolved.Artifact.OutputName
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	http.ServeFile(w, r, resolved.StoredPath)
}

func (a *App) handleRunAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var request runRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	result, runErr := engine.New(a.project, a.database).RunPipeline(a.ctx, request.Pipeline)
	if result == nil {
		writeAPIError(w, http.StatusBadRequest, runErr)
		return
	}
	response := runResponse{
		Run: func() *runSummary {
			summary := runSummary{
				ID:         result.RunID,
				Pipeline:   result.Pipeline,
				Status:     result.Status,
				StartedAt:  result.Manifest.StartedAt,
				EndedAt:    result.Manifest.EndedAt,
				DurationMS: durationMS(result.Manifest.StartedAt, result.Manifest.EndedAt),
			}
			return &summary
		}(),
	}
	if runErr != nil {
		response.Error = runErr.Error()
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var request publishRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if filepath.IsAbs(request.Path) {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("publish path must be project-relative"))
		return
	}
	resolved, _, err := a.resolveReference(request.Ref)
	if err != nil {
		status := http.StatusInternalServerError
		if db.IsNotFound(err) {
			status = http.StatusNotFound
		}
		writeAPIError(w, status, err)
		return
	}
	if resolved.Artifact == nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("publish requires an artifact ref"))
		return
	}
	target, err := fsx.SafeJoin(a.project.Root, request.Path)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if err := workspace.Materialize(resolved.StoredPath, target, workspace.Mode(a.project.Config.PublishMode)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, publishResponse{Path: target})
}

func (a *App) resolveReference(raw string) (resolve.RefResult, string, error) {
	ref, err := resolve.Alias(a.database, raw)
	if err != nil {
		return resolve.RefResult{}, "", err
	}
	resolved, err := resolve.Ref(a.project.Root, a.database, ref)
	if err != nil {
		return resolve.RefResult{}, "", err
	}
	return resolved, ref.String(), nil
}

func (a *App) stepDetail(pipelineName string, record db.StepRecord) (stepSummary, error) {
	artifacts, err := a.database.ListArtifacts(record.RunID, record.StepName)
	if err != nil {
		return stepSummary{}, err
	}
	item := summarizeStep(record)
	for _, artifact := range artifacts {
		path, err := a.store.Resolve(artifact.ObjectRef)
		if err != nil {
			return stepSummary{}, err
		}
		view, err := a.artifactDetail(refForArtifact(record.RunID, record.StepName, artifact.OutputName), artifact, path)
		if err != nil {
			return stepSummary{}, err
		}
		item.Artifacts = append(item.Artifacts, view)
	}
	item.Stdout, item.StdoutShort, err = a.readObjectText(record.StdoutObjectRef, maxLogPreviewByte)
	if err != nil {
		return stepSummary{}, err
	}
	item.Stderr, item.StderrShort, err = a.readObjectText(record.StderrObjectRef, maxLogPreviewByte)
	if err != nil {
		return stepSummary{}, err
	}
	_ = pipelineName
	return item, nil
}

func (a *App) artifactDetail(ref string, record db.ArtifactRecord, storedPath string) (artifactView, error) {
	preview, truncated, err := safePreview(storedPath, record.ArtifactType, maxPreviewBytes)
	if err != nil {
		return artifactView{}, err
	}
	return artifactView{
		Ref:          ref,
		RunID:        record.RunID,
		StepName:     record.StepName,
		OutputName:   record.OutputName,
		ArtifactType: record.ArtifactType,
		ObjectRef:    record.ObjectRef,
		StoredPath:   storedPath,
		SizeBytes:    record.SizeBytes,
		CreatedAt:    record.CreatedAt,
		Preview:      preview,
		PreviewShort: truncated,
	}, nil
}

func (a *App) lineageForArtifact(ref string, record db.ArtifactRecord, storedPath string) (artifactLineage, error) {
	item, err := a.artifactDetail(ref, record, storedPath)
	if err != nil {
		return artifactLineage{}, err
	}
	edges, err := a.database.ListIncomingProvenance(record.ID)
	if err != nil {
		return artifactLineage{}, err
	}
	lineage := artifactLineage{Artifact: item}
	for _, edge := range edges {
		sourcePath, err := a.store.Resolve(edge.From.ObjectRef)
		if err != nil {
			return artifactLineage{}, err
		}
		fromRef := refForArtifact(edge.From.RunID, edge.From.StepName, edge.From.OutputName)
		source, err := a.artifactDetail(fromRef, edge.From, sourcePath)
		if err != nil {
			return artifactLineage{}, err
		}
		lineage.Inputs = append(lineage.Inputs, provenanceEdge{
			ViaStep: edge.Via,
			From:    source,
			FromRef: fromRef,
		})
	}
	return lineage, nil
}

func (a *App) readObjectText(objectRef string, limit int) (string, bool, error) {
	if strings.TrimSpace(objectRef) == "" {
		return "", false, nil
	}
	path, err := a.store.Resolve(objectRef)
	if err != nil {
		return "", false, err
	}
	data, truncated, err := readLimited(path, limit)
	if err != nil {
		return "", false, err
	}
	return string(data), truncated, nil
}

func summarizeRuns(records []db.RunRecord) []runSummary {
	var out []runSummary
	for _, record := range records {
		out = append(out, summarizeRun(record))
	}
	return out
}

func summarizeRun(record db.RunRecord) runSummary {
	return runSummary{
		ID:         record.ID,
		Pipeline:   record.Pipeline,
		Status:     record.Status,
		StartedAt:  record.StartedAt,
		EndedAt:    record.EndedAt,
		DurationMS: durationMS(record.StartedAt, record.EndedAt),
	}
}

func summarizeSteps(records []db.StepRecord) []stepSummary {
	var out []stepSummary
	for _, record := range records {
		out = append(out, summarizeStep(record))
	}
	return out
}

func summarizeStep(record db.StepRecord) stepSummary {
	return stepSummary{
		RunID:      record.RunID,
		StepName:   record.StepName,
		Status:     record.Status,
		Command:    record.Command,
		ExitCode:   record.ExitCode,
		StartedAt:  record.StartedAt,
		EndedAt:    record.EndedAt,
		DurationMS: durationMS(record.StartedAt, record.EndedAt),
		StdoutRef:  record.StdoutObjectRef,
		StderrRef:  record.StderrObjectRef,
	}
}

func summarizeAliases(records []db.AliasRecord) []aliasSummary {
	var out []aliasSummary
	for _, record := range records {
		out = append(out, aliasSummary{
			Name:      record.Name,
			TargetRef: record.TargetRef,
			UpdatedAt: record.UpdatedAt,
		})
	}
	return out
}

func summarizeStepSpecs(steps []pipeline.Step) []stepSpecView {
	var out []stepSpecView
	for _, step := range steps {
		item := stepSpecView{
			Name:    step.Name,
			Kind:    step.Kind,
			Command: step.Run,
		}
		for _, input := range step.Inputs {
			item.Inputs = append(item.Inputs, inputSpecView{
				Name:   input.Name,
				Source: input.Source,
				From:   input.From,
				Ref:    input.Ref,
			})
			if input.From != "" {
				prevStep, _, err := pipeline.ParseStepOutputRef(input.From)
				if err == nil {
					item.Dependencies = append(item.Dependencies, prevStep)
				}
			}
		}
		for _, output := range step.Outputs {
			item.Outputs = append(item.Outputs, outputSpecView{
				Name:    output.Name,
				Path:    output.Path,
				Type:    output.Type,
				Publish: output.Publish,
			})
		}
		out = append(out, item)
	}
	return out
}

func safePreview(path, artifactType string, limit int) (string, bool, error) {
	if artifactType != "file" {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", false, err
		}
		var names []string
		for i, entry := range entries {
			if i >= 20 {
				names = append(names, "...")
				break
			}
			names = append(names, entry.Name())
		}
		return strings.Join(names, "\n"), len(entries) > 20, nil
	}
	data, truncated, err := readLimited(path, limit)
	if err != nil {
		return "", false, err
	}
	if len(data) == 0 {
		return "", truncated, nil
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", false, nil
	}
	return string(data), truncated, nil
}

func readLimited(path string, limit int) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	reader := io.LimitReader(file, int64(limit)+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, false, err
	}
	if len(data) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func refForArtifact(runID, stepName, outputName string) string {
	return fmt.Sprintf("run:%s:%s/%s", runID, stepName, outputName)
}

func durationMS(startedAt, endedAt time.Time) int64 {
	if startedAt.IsZero() || endedAt.IsZero() {
		return 0
	}
	return endedAt.Sub(startedAt).Milliseconds()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func listenerURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

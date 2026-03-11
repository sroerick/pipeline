package manifest

import "time"

type Run struct {
	RunID        string    `json:"run_id"`
	PipelineName string    `json:"pipeline_name"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	Status       string    `json:"status"`
	Steps        []Step    `json:"steps"`
}

type Step struct {
	StepName   string    `json:"step_name"`
	Command    string    `json:"command"`
	Inputs     []Input   `json:"inputs"`
	Outputs    []Output  `json:"outputs"`
	ExitCode   int       `json:"exit_code"`
	StdoutRef  string    `json:"stdout_ref"`
	StderrRef  string    `json:"stderr_ref"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	Status     string    `json:"status"`
	WorkingDir string    `json:"working_dir"`
	Error      string    `json:"error,omitempty"`
}

type Input struct {
	Kind         string `json:"kind"`
	Source       string `json:"source,omitempty"`
	Ref          string `json:"ref,omitempty"`
	ResolvedPath string `json:"resolved_path,omitempty"`
	ObjectRef    string `json:"object_ref,omitempty"`
}

type Output struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	ObjectRef  string `json:"object_ref,omitempty"`
	StoredPath string `json:"stored_path,omitempty"`
	SizeBytes  int64  `json:"size_bytes"`
}

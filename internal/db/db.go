package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found")

const sqliteBusyTimeoutMS = 5000

type DB struct {
	path string
}

type RunRecord struct {
	ID        string
	Pipeline  string
	Status    string
	StartedAt time.Time
	EndedAt   time.Time
}

type StepRecord struct {
	ID              int64
	RunID           string
	StepName        string
	Status          string
	Command         string
	ExitCode        int
	StdoutObjectRef string
	StderrObjectRef string
	StartedAt       time.Time
	EndedAt         time.Time
}

type ArtifactRecord struct {
	ID           int64
	RunID        string
	StepName     string
	OutputName   string
	ArtifactType string
	ObjectRef    string
	SizeBytes    int64
	CreatedAt    time.Time
}

type AliasRecord struct {
	Name      string
	TargetRef string
	UpdatedAt time.Time
}

type ProvenanceEdge struct {
	From ArtifactRecord
	To   ArtifactRecord
	Via  string
}

func Init(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	database := &DB{path: path}
	return database.execSQL(strings.Join([]string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA synchronous = NORMAL;`,
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			pipeline_name TEXT NOT NULL,
			status TEXT NOT NULL,
			started_at TEXT NOT NULL,
			ended_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS steps (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			step_name TEXT NOT NULL,
			status TEXT NOT NULL,
			command TEXT NOT NULL,
			exit_code INTEGER NOT NULL,
			stdout_object_ref TEXT,
			stderr_object_ref TEXT,
			started_at TEXT NOT NULL,
			ended_at TEXT NOT NULL,
			UNIQUE(run_id, step_name)
		);`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			step_name TEXT NOT NULL,
			output_name TEXT NOT NULL,
			artifact_type TEXT NOT NULL,
			object_ref TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(run_id, step_name, output_name)
		);`,
		`CREATE TABLE IF NOT EXISTS aliases (
			name TEXT PRIMARY KEY,
			target_ref TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS provenance_edges (
			from_artifact_id INTEGER NOT NULL,
			to_artifact_id INTEGER NOT NULL,
			via_step_name TEXT NOT NULL,
			PRIMARY KEY(from_artifact_id, to_artifact_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_pipeline_started_at ON runs (pipeline_name, started_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_steps_run ON steps (run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_run_step ON artifacts (run_id, step_name);`,
		`CREATE INDEX IF NOT EXISTS idx_provenance_to_artifact ON provenance_edges (to_artifact_id);`,
	}, "\n"))
}

func Open(path string) (*DB, error) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, fmt.Errorf("sqlite3 not found in PATH")
	}
	database := &DB{path: path}
	if err := database.configure(); err != nil {
		return nil, err
	}
	return database, nil
}

func (d *DB) Close() error {
	return nil
}

func (d *DB) configure() error {
	return d.execSQL(strings.Join([]string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA synchronous = NORMAL;`,
	}, "\n"))
}

func (d *DB) CreateRun(record RunRecord) error {
	return d.execSQL(fmt.Sprintf(
		`INSERT INTO runs(id, pipeline_name, status, started_at, ended_at) VALUES (%s, %s, %s, %s, %s);`,
		quote(record.ID), quote(record.Pipeline), quote(record.Status), quoteTime(record.StartedAt), nullableTime(record.EndedAt),
	))
}

func (d *DB) FinishRun(runID, status string, endedAt time.Time) error {
	return d.execSQL(fmt.Sprintf(
		`UPDATE runs SET status = %s, ended_at = %s WHERE id = %s;`,
		quote(status), quoteTime(endedAt), quote(runID),
	))
}

func (d *DB) UpsertStep(record StepRecord) error {
	return d.execSQL(fmt.Sprintf(`
		INSERT INTO steps(run_id, step_name, status, command, exit_code, stdout_object_ref, stderr_object_ref, started_at, ended_at)
		VALUES (%s, %s, %s, %s, %d, %s, %s, %s, %s)
		ON CONFLICT(run_id, step_name) DO UPDATE SET
			status = excluded.status,
			command = excluded.command,
			exit_code = excluded.exit_code,
			stdout_object_ref = excluded.stdout_object_ref,
			stderr_object_ref = excluded.stderr_object_ref,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at;
	`,
		quote(record.RunID), quote(record.StepName), quote(record.Status), quote(record.Command), record.ExitCode,
		nullableString(record.StdoutObjectRef), nullableString(record.StderrObjectRef),
		quoteTime(record.StartedAt), quoteTime(record.EndedAt),
	))
}

func (d *DB) InsertArtifact(record ArtifactRecord) (int64, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		INSERT INTO artifacts(run_id, step_name, output_name, artifact_type, object_ref, size_bytes, created_at)
		VALUES (%s, %s, %s, %s, %s, %d, %s);
		SELECT last_insert_rowid() AS id;
	`,
		quote(record.RunID), quote(record.StepName), quote(record.OutputName), quote(record.ArtifactType),
		quote(record.ObjectRef), record.SizeBytes, quoteTime(record.CreatedAt),
	))
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, ErrNotFound
	}
	return parseInt64(rows[0]["id"])
}

func (d *DB) InsertProvenanceEdge(fromID, toID int64, viaStep string) error {
	return d.execSQL(fmt.Sprintf(
		`INSERT OR IGNORE INTO provenance_edges(from_artifact_id, to_artifact_id, via_step_name) VALUES (%d, %d, %s);`,
		fromID, toID, quote(viaStep),
	))
}

func (d *DB) SetAlias(name, targetRef string, updatedAt time.Time) error {
	return d.execSQL(fmt.Sprintf(`
		INSERT INTO aliases(name, target_ref, updated_at) VALUES (%s, %s, %s)
		ON CONFLICT(name) DO UPDATE SET target_ref = excluded.target_ref, updated_at = excluded.updated_at;
	`, quote(name), quote(targetRef), quoteTime(updatedAt)))
}

func (d *DB) GetAlias(name string) (AliasRecord, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		SELECT name, target_ref, updated_at FROM aliases WHERE name = %s;
	`, quote(name)))
	if err != nil {
		return AliasRecord{}, err
	}
	if len(rows) == 0 {
		return AliasRecord{}, ErrNotFound
	}
	return aliasFromRow(rows[0])
}

func (d *DB) ListAliases() ([]AliasRecord, error) {
	rows, err := d.queryRows(`SELECT name, target_ref, updated_at FROM aliases ORDER BY name;`)
	if err != nil {
		return nil, err
	}
	var out []AliasRecord
	for _, row := range rows {
		rec, err := aliasFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (d *DB) ListRuns(limit int, pipelineName string) ([]RunRecord, error) {
	query := `SELECT id, pipeline_name, status, started_at, IFNULL(ended_at, '') AS ended_at FROM runs`
	if pipelineName != "" {
		query += ` WHERE pipeline_name = ` + quote(pipelineName)
	}
	query += ` ORDER BY started_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	query += `;`
	rows, err := d.queryRows(query)
	if err != nil {
		return nil, err
	}
	var out []RunRecord
	for _, row := range rows {
		rec, err := runFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (d *DB) GetRun(runID string) (RunRecord, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		SELECT id, pipeline_name, status, started_at, IFNULL(ended_at, '') AS ended_at
		FROM runs
		WHERE id = %s;
	`, quote(runID)))
	if err != nil {
		return RunRecord{}, err
	}
	if len(rows) == 0 {
		return RunRecord{}, ErrNotFound
	}
	return runFromRow(rows[0])
}

func (d *DB) GetLatestRunForPipeline(pipelineName string) (RunRecord, error) {
	rows, err := d.ListRuns(1, pipelineName)
	if err != nil {
		return RunRecord{}, err
	}
	if len(rows) == 0 {
		return RunRecord{}, ErrNotFound
	}
	return rows[0], nil
}

func (d *DB) GetLatestSuccessfulRunForPipeline(pipelineName string) (RunRecord, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		SELECT id, pipeline_name, status, started_at, IFNULL(ended_at, '') AS ended_at
		FROM runs
		WHERE pipeline_name = %s AND status = 'success'
		ORDER BY started_at DESC
		LIMIT 1;
	`, quote(pipelineName)))
	if err != nil {
		return RunRecord{}, err
	}
	if len(rows) == 0 {
		return RunRecord{}, ErrNotFound
	}
	return runFromRow(rows[0])
}

func (d *DB) ListSteps(runID string) ([]StepRecord, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		SELECT id, run_id, step_name, status, command, exit_code, IFNULL(stdout_object_ref, '') AS stdout_object_ref,
			IFNULL(stderr_object_ref, '') AS stderr_object_ref, started_at, ended_at
		FROM steps
		WHERE run_id = %s
		ORDER BY started_at ASC;
	`, quote(runID)))
	if err != nil {
		return nil, err
	}
	var out []StepRecord
	for _, row := range rows {
		rec, err := stepFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (d *DB) GetStep(runID, step string) (StepRecord, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		SELECT id, run_id, step_name, status, command, exit_code, IFNULL(stdout_object_ref, '') AS stdout_object_ref,
			IFNULL(stderr_object_ref, '') AS stderr_object_ref, started_at, ended_at
		FROM steps
		WHERE run_id = %s AND step_name = %s;
	`, quote(runID), quote(step)))
	if err != nil {
		return StepRecord{}, err
	}
	if len(rows) == 0 {
		return StepRecord{}, ErrNotFound
	}
	return stepFromRow(rows[0])
}

func (d *DB) ListArtifacts(runID, step string) ([]ArtifactRecord, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		SELECT id, run_id, step_name, output_name, artifact_type, object_ref, size_bytes, created_at
		FROM artifacts
		WHERE run_id = %s AND step_name = %s
		ORDER BY output_name ASC;
	`, quote(runID), quote(step)))
	if err != nil {
		return nil, err
	}
	var out []ArtifactRecord
	for _, row := range rows {
		rec, err := artifactFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (d *DB) GetArtifact(runID, step, output string) (ArtifactRecord, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		SELECT id, run_id, step_name, output_name, artifact_type, object_ref, size_bytes, created_at
		FROM artifacts
		WHERE run_id = %s AND step_name = %s AND output_name = %s;
	`, quote(runID), quote(step), quote(output)))
	if err != nil {
		return ArtifactRecord{}, err
	}
	if len(rows) == 0 {
		return ArtifactRecord{}, ErrNotFound
	}
	return artifactFromRow(rows[0])
}

func (d *DB) ListFailedSteps(limit int) ([]StepRecord, error) {
	query := `
		SELECT id, run_id, step_name, status, command, exit_code, IFNULL(stdout_object_ref, '') AS stdout_object_ref,
			IFNULL(stderr_object_ref, '') AS stderr_object_ref, started_at, ended_at
		FROM steps
		WHERE status = 'failed'
		ORDER BY started_at DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	query += ";"
	rows, err := d.queryRows(query)
	if err != nil {
		return nil, err
	}
	var out []StepRecord
	for _, row := range rows {
		rec, err := stepFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (d *DB) ListIncomingProvenance(toArtifactID int64) ([]ProvenanceEdge, error) {
	rows, err := d.queryRows(fmt.Sprintf(`
		SELECT
			f.id AS from_id, f.run_id AS from_run_id, f.step_name AS from_step_name, f.output_name AS from_output_name,
			f.artifact_type AS from_artifact_type, f.object_ref AS from_object_ref, f.size_bytes AS from_size_bytes,
			f.created_at AS from_created_at,
			t.id AS to_id, t.run_id AS to_run_id, t.step_name AS to_step_name, t.output_name AS to_output_name,
			t.artifact_type AS to_artifact_type, t.object_ref AS to_object_ref, t.size_bytes AS to_size_bytes,
			t.created_at AS to_created_at,
			p.via_step_name AS via_step_name
		FROM provenance_edges p
		JOIN artifacts f ON f.id = p.from_artifact_id
		JOIN artifacts t ON t.id = p.to_artifact_id
		WHERE p.to_artifact_id = %d
		ORDER BY f.step_name, f.output_name;
	`, toArtifactID))
	if err != nil {
		return nil, err
	}
	var out []ProvenanceEdge
	for _, row := range rows {
		fromID, err := parseInt64(row["from_id"])
		if err != nil {
			return nil, err
		}
		toID, err := parseInt64(row["to_id"])
		if err != nil {
			return nil, err
		}
		fromCreated, err := parseTime(row["from_created_at"])
		if err != nil {
			return nil, err
		}
		toCreated, err := parseTime(row["to_created_at"])
		if err != nil {
			return nil, err
		}
		fromSize, err := parseInt64(row["from_size_bytes"])
		if err != nil {
			return nil, err
		}
		toSize, err := parseInt64(row["to_size_bytes"])
		if err != nil {
			return nil, err
		}
		out = append(out, ProvenanceEdge{
			From: ArtifactRecord{
				ID:           fromID,
				RunID:        row["from_run_id"],
				StepName:     row["from_step_name"],
				OutputName:   row["from_output_name"],
				ArtifactType: row["from_artifact_type"],
				ObjectRef:    row["from_object_ref"],
				SizeBytes:    fromSize,
				CreatedAt:    fromCreated,
			},
			To: ArtifactRecord{
				ID:           toID,
				RunID:        row["to_run_id"],
				StepName:     row["to_step_name"],
				OutputName:   row["to_output_name"],
				ArtifactType: row["to_artifact_type"],
				ObjectRef:    row["to_object_ref"],
				SizeBytes:    toSize,
				CreatedAt:    toCreated,
			},
			Via: row["via_step_name"],
		})
	}
	return out, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

func (d *DB) execSQL(sql string) error {
	cmd := d.sqliteCommand(sql, false)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, trimmed)
	}
	return nil
}

func (d *DB) queryRows(sql string) ([]map[string]string, error) {
	cmd := d.sqliteCommand(sql, true)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, trimmed)
	}
	if strings.TrimSpace(string(output)) == "" {
		return nil, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, err
	}
	var out []map[string]string
	for _, row := range rows {
		converted := map[string]string{}
		for key, value := range row {
			switch v := value.(type) {
			case nil:
				converted[key] = ""
			case string:
				converted[key] = v
			case float64:
				if v == float64(int64(v)) {
					converted[key] = strconv.FormatInt(int64(v), 10)
				} else {
					converted[key] = strconv.FormatFloat(v, 'f', -1, 64)
				}
			case bool:
				converted[key] = strconv.FormatBool(v)
			default:
				converted[key] = fmt.Sprint(v)
			}
		}
		out = append(out, converted)
	}
	return out, nil
}

func (d *DB) sqliteCommand(sql string, jsonOutput bool) *exec.Cmd {
	args := []string{
		"-batch",
		"-cmd", fmt.Sprintf(".timeout %d", sqliteBusyTimeoutMS),
		"-cmd", "PRAGMA foreign_keys = ON;",
	}
	if jsonOutput {
		args = append(args, "-json")
	}
	args = append(args, d.path, sql)
	return exec.Command("sqlite3", args...)
}

func runFromRow(row map[string]string) (RunRecord, error) {
	startedAt, err := parseTime(row["started_at"])
	if err != nil {
		return RunRecord{}, err
	}
	endedAt, err := parseOptionalTime(row["ended_at"])
	if err != nil {
		return RunRecord{}, err
	}
	return RunRecord{
		ID:        row["id"],
		Pipeline:  row["pipeline_name"],
		Status:    row["status"],
		StartedAt: startedAt,
		EndedAt:   endedAt,
	}, nil
}

func stepFromRow(row map[string]string) (StepRecord, error) {
	id, err := parseInt64(row["id"])
	if err != nil {
		return StepRecord{}, err
	}
	exitCode, err := strconv.Atoi(row["exit_code"])
	if err != nil {
		return StepRecord{}, err
	}
	startedAt, err := parseTime(row["started_at"])
	if err != nil {
		return StepRecord{}, err
	}
	endedAt, err := parseTime(row["ended_at"])
	if err != nil {
		return StepRecord{}, err
	}
	return StepRecord{
		ID:              id,
		RunID:           row["run_id"],
		StepName:        row["step_name"],
		Status:          row["status"],
		Command:         row["command"],
		ExitCode:        exitCode,
		StdoutObjectRef: row["stdout_object_ref"],
		StderrObjectRef: row["stderr_object_ref"],
		StartedAt:       startedAt,
		EndedAt:         endedAt,
	}, nil
}

func artifactFromRow(row map[string]string) (ArtifactRecord, error) {
	id, err := parseInt64(row["id"])
	if err != nil {
		return ArtifactRecord{}, err
	}
	sizeBytes, err := parseInt64(row["size_bytes"])
	if err != nil {
		return ArtifactRecord{}, err
	}
	createdAt, err := parseTime(row["created_at"])
	if err != nil {
		return ArtifactRecord{}, err
	}
	return ArtifactRecord{
		ID:           id,
		RunID:        row["run_id"],
		StepName:     row["step_name"],
		OutputName:   row["output_name"],
		ArtifactType: row["artifact_type"],
		ObjectRef:    row["object_ref"],
		SizeBytes:    sizeBytes,
		CreatedAt:    createdAt,
	}, nil
}

func aliasFromRow(row map[string]string) (AliasRecord, error) {
	updatedAt, err := parseTime(row["updated_at"])
	if err != nil {
		return AliasRecord{}, err
	}
	return AliasRecord{
		Name:      row["name"],
		TargetRef: row["target_ref"],
		UpdatedAt: updatedAt,
	}, nil
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func parseOptionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseTime(value)
}

func parseInt64(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quoteTime(value time.Time) string {
	return quote(value.UTC().Format(time.RFC3339Nano))
}

func nullableTime(value time.Time) string {
	if value.IsZero() {
		return "NULL"
	}
	return quoteTime(value)
}

func nullableString(value string) string {
	if value == "" {
		return "NULL"
	}
	return quote(value)
}

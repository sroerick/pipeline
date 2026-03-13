package webui_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pakkun/internal/config"
	"pakkun/internal/db"
	"pakkun/internal/webui"
)

func TestUIServerRunAndArtifactFlow(t *testing.T) {
	projectDir := t.TempDir()
	writeFile(t, filepath.Join(projectDir, "input.txt"), "hello pipeline\n")
	writeFile(t, filepath.Join(projectDir, "pipe.yaml"), `
version: 1

pipelines:
  - name: demo
    steps:
      - name: copy
        kind: shell
        run: cat input.txt > "$PIPE_STEP_OUT/copied.txt"
        inputs:
          - source: input.txt
        outputs:
          - name: text
            path: copied.txt
            type: file

      - name: upper
        kind: shell
        run: tr '[:lower:]' '[:upper:]' < "$PIPE_INPUT_text" > "$PIPE_STEP_OUT/result.txt"
        inputs:
          - from: copy/text
        outputs:
          - name: result
            path: result.txt
            type: file
`)

	project, err := config.Init(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(config.DBPath(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := webui.Handler(ctx, project, database)

	indexResp := doRequest(t, handler, http.MethodGet, "/", nil)
	indexBody := readBody(t, indexResp)
	if !strings.Contains(indexBody, "pakkun ui") {
		t.Fatalf("index did not contain ui title: %q", indexBody)
	}

	runPayload := doJSON(t, handler, http.MethodPost, "/api/run", map[string]string{"pipeline": "demo"})
	runID := nestedString(t, runPayload, "run", "id")
	if runID == "" {
		t.Fatalf("missing run id in payload: %#v", runPayload)
	}

	statusPayload := doJSON(t, handler, http.MethodGet, "/api/status", nil)
	latestRuns, ok := statusPayload["latest_runs"].([]any)
	if !ok || len(latestRuns) != 1 {
		t.Fatalf("latest_runs = %#v", statusPayload["latest_runs"])
	}

	runDetail := doJSON(t, handler, http.MethodGet, "/api/runs/"+runID, nil)
	steps, ok := runDetail["steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("steps = %#v", runDetail["steps"])
	}

	artifactPayload := doJSON(t, handler, http.MethodGet, "/api/artifact?ref=demo%3Aupper%2Fresult", nil)
	if got := stringValue(artifactPayload["preview"]); got != "HELLO PIPELINE\n" {
		t.Fatalf("artifact preview = %q", got)
	}

	provenancePayload := doJSON(t, handler, http.MethodGet, "/api/provenance?ref=demo%3Aupper%2Fresult", nil)
	artifacts, ok := provenancePayload["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("artifacts = %#v", provenancePayload["artifacts"])
	}

	publishPayload := doJSON(t, handler, http.MethodPost, "/api/publish", map[string]string{
		"ref":  "demo:upper/result",
		"path": "exports/result.txt",
	})
	publishedPath := stringValue(publishPayload["path"])
	data, err := os.ReadFile(publishedPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "HELLO PIPELINE\n"; got != want {
		t.Fatalf("published content = %q, want %q", got, want)
	}

	if err := os.MkdirAll(filepath.Join(projectDir, "existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	conflictResp := doRequest(t, handler, http.MethodPost, "/api/publish", map[string]string{
		"ref":  "demo:upper/result",
		"path": "existing",
	})
	if conflictResp.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status = %d, want %d: %s", conflictResp.StatusCode, http.StatusConflict, readBody(t, conflictResp))
	}
}

func doJSON(t *testing.T, handler http.Handler, method, path string, body map[string]string) map[string]any {
	t.Helper()
	resp := doRequest(t, handler, method, path, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s returned %d: %s", method, path, resp.StatusCode, readBody(t, resp))
	}
	return decodeBody(t, resp)
}

func doRequest(t *testing.T, handler http.Handler, method, path string, body map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Result()
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func nestedString(t *testing.T, value map[string]any, keys ...string) string {
	t.Helper()
	current := any(value)
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("value at %v was not a map: %#v", keys, current)
		}
		current = asMap[key]
	}
	return stringValue(current)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

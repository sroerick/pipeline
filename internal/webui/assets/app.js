const state = {
  status: null
};

const els = {
  projectRoot: document.getElementById("project-root"),
  pipelineList: document.getElementById("pipeline-list"),
  view: document.getElementById("view"),
  viewTitle: document.getElementById("view-title"),
  flash: document.getElementById("flash"),
  pageActions: document.getElementById("page-actions"),
  refreshButton: document.getElementById("refresh-button")
};

els.refreshButton.addEventListener("click", async () => {
  await refreshStatus();
  await renderRoute();
});

window.addEventListener("hashchange", () => {
  void renderRoute();
});

void bootstrap();

async function bootstrap() {
  await refreshStatus();
  await renderRoute();
}

async function refreshStatus() {
  state.status = await fetchJSON("/api/status");
  renderSidebar();
}

function renderSidebar() {
  if (!state.status) {
    return;
  }
  els.projectRoot.textContent = state.status.project_root;
  els.pipelineList.innerHTML = "";
  for (const pipeline of state.status.pipelines) {
    const link = document.createElement("a");
    link.className = "pipeline-link";
    link.href = `#/pipeline/${encodeURIComponent(pipeline.name)}`;
    if (decodeURIComponent(location.hash.replace(/^#\/pipeline\//, "")) === pipeline.name) {
      link.classList.add("active");
    }
    const latest = pipeline.latest_run
      ? `latest ${pipeline.latest_run.status} • ${formatDateTime(pipeline.latest_run.started_at)}`
      : "no runs yet";
    link.innerHTML = `
      <strong>${escapeHTML(pipeline.name)}</strong>
      <span>${pipeline.step_count} steps</span>
      <span>${escapeHTML(latest)}</span>
    `;
    els.pipelineList.appendChild(link);
  }
}

async function renderRoute() {
  clearFlash();
  const route = parseRoute();
  try {
    if (route.type === "overview") {
      await renderOverview();
      return;
    }
    if (route.type === "pipeline") {
      await renderPipeline(route.name);
      return;
    }
    if (route.type === "run") {
      await renderRun(route.id);
      return;
    }
    if (route.type === "artifact") {
      await renderArtifact(route.ref);
      return;
    }
    await renderOverview();
  } catch (error) {
    showFlash(error.message || String(error), true);
  }
}

function parseRoute() {
  const hash = location.hash || "#/";
  const cleaned = hash.startsWith("#") ? hash.slice(1) : hash;
  if (cleaned === "/" || cleaned === "") {
    return { type: "overview" };
  }
  const parts = cleaned.replace(/^\//, "").split("/");
  if (parts[0] === "pipeline" && parts[1]) {
    return { type: "pipeline", name: decodeURIComponent(parts.slice(1).join("/")) };
  }
  if (parts[0] === "run" && parts[1]) {
    return { type: "run", id: decodeURIComponent(parts.slice(1).join("/")) };
  }
  if (parts[0] === "artifact" && parts[1]) {
    return { type: "artifact", ref: decodeURIComponent(parts.slice(1).join("/")) };
  }
  return { type: "overview" };
}

async function renderOverview() {
  els.viewTitle.textContent = "Overview";
  els.pageActions.innerHTML = "";
  renderOverviewActions();
  const status = state.status;
  const latestRuns = status.latest_runs || [];
  const failedSteps = status.failed_steps || [];
  const aliases = status.aliases || [];
  const pipelineCards = status.pipelines
    .map((pipeline) => `
      <div class="list-item">
        <div class="meta-row">
          <strong>${escapeHTML(pipeline.name)}</strong>
          <span class="chip">${pipeline.step_count} steps</span>
        </div>
        <p class="muted">${escapeHTML(
          pipeline.latest_run
            ? `Latest run ${pipeline.latest_run.id} is ${pipeline.latest_run.status}`
            : "No runs yet"
        )}</p>
        <div class="run-actions">
          <a class="button ghost" href="#/pipeline/${encodeURIComponent(pipeline.name)}">Open pipeline</a>
          <button type="button" data-run-pipeline="${escapeAttr(pipeline.name)}">Run</button>
        </div>
      </div>
    `)
    .join("");

  els.view.innerHTML = `
    <div class="grid two">
      <section class="card">
        <h3>Project</h3>
        <div class="stack">
          <p><strong>Root</strong><br><span class="muted code">${escapeHTML(status.project_root)}</span></p>
          <p><strong>Spec</strong><br><span class="muted code">${escapeHTML(status.spec_path)}</span></p>
        </div>
      </section>
      <section class="card">
        <h3>Aliases</h3>
        ${
          aliases.length > 0
            ? `<div class="list">${aliases
                .map(
                  (alias) => `<div class="list-item"><strong>${escapeHTML(alias.name)}</strong><p class="muted code">${escapeHTML(alias.target_ref)}</p></div>`
                )
                .join("")}</div>`
            : `<div class="empty">No aliases recorded yet.</div>`
        }
      </section>
    </div>
    <section class="card">
      <h3>Pipelines</h3>
      ${pipelineCards ? `<div class="list">${pipelineCards}</div>` : `<div class="empty">No pipelines found.</div>`}
    </section>
    <div class="grid two">
      <section class="card">
        <h3>Latest runs</h3>
        ${
          latestRuns.length > 0
            ? `<div class="list">${latestRuns.map(renderRunListItem).join("")}</div>`
            : `<div class="empty">No runs yet.</div>`
        }
      </section>
      <section class="card">
        <h3>Failed steps</h3>
        ${
          failedSteps.length > 0
            ? `<div class="list">${failedSteps
                .map(
                  (step) => `
                    <div class="list-item">
                      <div class="meta-row">
                        <strong>${escapeHTML(step.step_name)}</strong>
                        <span class="chip status-${escapeAttr(step.status)}">${escapeHTML(step.status)}</span>
                      </div>
                      <p class="muted code">${escapeHTML(step.run_id)}</p>
                    </div>
                  `
                )
                .join("")}</div>`
            : `<div class="empty">No failed steps.</div>`
        }
      </section>
    </div>
  `;
  bindRunButtons();
}

function renderOverviewActions() {
  const actionWrap = document.createElement("div");
  actionWrap.className = "actions";
  if (state.status?.pipelines?.length) {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = "Run a pipeline";
    button.addEventListener("click", async () => {
      const choice = prompt("Pipeline name to run", state.status.pipelines[0].name || "");
      if (!choice) {
        return;
      }
      await runPipeline(choice);
    });
    actionWrap.appendChild(button);
  }
  els.pageActions.appendChild(actionWrap);
}

async function renderPipeline(name) {
  const pipeline = await fetchJSON(`/api/pipelines/${encodeURIComponent(name)}`);
  els.viewTitle.textContent = pipeline.name;
  els.pageActions.innerHTML = `
    <button type="button" id="run-current-pipeline">Run ${escapeHTML(pipeline.name)}</button>
    ${
      pipeline.latest_run
        ? `<a class="button ghost" href="#/run/${encodeURIComponent(pipeline.latest_run.id)}">Latest run</a>`
        : ""
    }
  `;
  const steps = pipeline.steps
    .map(
      (step) => `
        <section class="card">
          <div class="meta-row">
            <h3>${escapeHTML(step.name)}</h3>
            <div class="chip-row">
              <span class="chip">${escapeHTML(step.kind || "shell")}</span>
              ${step.dependencies.map((dep) => `<span class="chip">needs ${escapeHTML(dep)}</span>`).join("")}
            </div>
          </div>
          <p class="muted code">${escapeHTML(step.command || "(no command)")}</p>
          <div class="grid two">
            <div>
              <p class="eyebrow">Inputs</p>
              ${
                step.inputs.length > 0
                  ? `<div class="list">${step.inputs
                      .map(
                        (input) => `
                          <div class="list-item">
                            <p class="code">${escapeHTML(input.from || input.ref || input.source || "(input)")}</p>
                          </div>
                        `
                      )
                      .join("")}</div>`
                  : `<p class="muted">No declared inputs.</p>`
              }
            </div>
            <div>
              <p class="eyebrow">Outputs</p>
              ${
                step.outputs.length > 0
                  ? `<div class="list">${step.outputs
                      .map(
                        (output) => `
                          <div class="list-item">
                            <strong>${escapeHTML(output.name)}</strong>
                            <p class="muted code">${escapeHTML(output.path)} • ${escapeHTML(output.type)}</p>
                            ${
                              output.publish
                                ? `<p class="muted">publish → <span class="code">${escapeHTML(output.publish)}</span></p>`
                                : ""
                            }
                          </div>
                        `
                      )
                      .join("")}</div>`
                  : `<p class="muted">No declared outputs.</p>`
              }
            </div>
          </div>
        </section>
      `
    )
    .join("");

  els.view.innerHTML = `
    ${
      pipeline.latest_run
        ? `<section class="card">
            <h3>Latest run</h3>
            ${renderRunListItem(pipeline.latest_run)}
          </section>`
        : ""
    }
    ${steps}
  `;
  document.getElementById("run-current-pipeline")?.addEventListener("click", async () => {
    await runPipeline(pipeline.name);
  });
}

async function renderRun(id) {
  const run = await fetchJSON(`/api/runs/${encodeURIComponent(id)}`);
  els.viewTitle.textContent = `Run ${run.run.id}`;
  els.pageActions.innerHTML = `
    <button type="button" id="rerun-pipeline">Run ${escapeHTML(run.run.pipeline)} again</button>
    <a class="button ghost" href="#/pipeline/${encodeURIComponent(run.run.pipeline)}">Pipeline</a>
  `;
  const steps = run.steps
    .map(
      (step) => `
        <section class="card">
          <div class="meta-row">
            <h3>${escapeHTML(step.step_name)}</h3>
            <div class="chip-row">
              <span class="chip status-${escapeAttr(step.status)}">${escapeHTML(step.status)}</span>
              <span class="chip">${formatDuration(step.duration_ms)}</span>
              <span class="chip">exit ${step.exit_code}</span>
            </div>
          </div>
          <p class="muted code">${escapeHTML(step.command)}</p>
          ${
            step.artifacts.length > 0
              ? `<div class="list">${step.artifacts
                  .map(
                    (artifact) => `
                      <div class="list-item">
                        <div class="meta-row">
                          <strong>${escapeHTML(artifact.output_name)}</strong>
                          <a href="#/artifact/${encodeURIComponent(artifact.ref)}">Open artifact</a>
                        </div>
                        <p class="muted code">${escapeHTML(artifact.stored_path)}</p>
                      </div>
                    `
                  )
                  .join("")}</div>`
              : `<p class="muted">No artifacts recorded.</p>`
          }
          <details>
            <summary>stdout ${step.stdout_truncated ? "(truncated)" : ""}</summary>
            <pre>${escapeHTML(step.stdout || "")}</pre>
          </details>
          <details>
            <summary>stderr ${step.stderr_truncated ? "(truncated)" : ""}</summary>
            <pre>${escapeHTML(step.stderr || "")}</pre>
          </details>
        </section>
      `
    )
    .join("");

  els.view.innerHTML = `
    <section class="card">
      <h3>${escapeHTML(run.run.pipeline)}</h3>
      <div class="chip-row">
        <span class="chip status-${escapeAttr(run.run.status)}">${escapeHTML(run.run.status)}</span>
        <span class="chip">${formatDateTime(run.run.started_at)}</span>
        <span class="chip">${formatDuration(run.run.duration_ms)}</span>
      </div>
    </section>
    ${steps}
    <section class="card">
      <h3>Manifest</h3>
      <p class="muted code">${escapeHTML(run.manifest.path)}</p>
      <pre>${escapeHTML(run.manifest.raw || "")}</pre>
    </section>
  `;
  document.getElementById("rerun-pipeline")?.addEventListener("click", async () => {
    await runPipeline(run.run.pipeline);
  });
}

async function renderArtifact(ref) {
  const [artifact, provenance] = await Promise.all([
    fetchJSON(`/api/artifact?ref=${encodeURIComponent(ref)}`),
    fetchJSON(`/api/provenance?ref=${encodeURIComponent(ref)}`)
  ]);
  els.viewTitle.textContent = `${artifact.step_name}/${artifact.output_name}`;
  els.pageActions.innerHTML = `
    <button type="button" id="publish-artifact">Publish</button>
    <a class="button ghost" href="/api/download?ref=${encodeURIComponent(ref)}">Download</a>
    <button type="button" class="ghost" id="copy-ref">Copy ref</button>
  `;
  const lineage = provenance.artifacts?.[0]?.inputs || [];
  els.view.innerHTML = `
    <section class="card">
      <h3>Artifact</h3>
      <div class="chip-row">
        <span class="chip">${escapeHTML(artifact.artifact_type)}</span>
        <span class="chip">${formatBytes(artifact.size_bytes)}</span>
      </div>
      <div class="stack">
        <p><strong>Ref</strong><br><span class="muted code">${escapeHTML(artifact.ref)}</span></p>
        <p><strong>Stored path</strong><br><span class="muted code">${escapeHTML(artifact.stored_path)}</span></p>
        <p><strong>Object</strong><br><span class="muted code">${escapeHTML(artifact.object_ref)}</span></p>
      </div>
    </section>
    <section class="card">
      <h3>Preview</h3>
      ${
        artifact.preview
          ? `<pre>${escapeHTML(artifact.preview)}</pre>`
          : `<div class="empty">No text preview available for this artifact.</div>`
      }
    </section>
    <section class="card">
      <h3>Provenance</h3>
      ${
        lineage.length > 0
          ? `<div class="list">${lineage
              .map(
                (edge) => `
                  <div class="list-item">
                    <div class="meta-row">
                      <strong>${escapeHTML(edge.via_step)}</strong>
                      <a href="#/artifact/${encodeURIComponent(edge.from_ref)}">Open source</a>
                    </div>
                    <p class="muted code">${escapeHTML(edge.from_ref)}</p>
                  </div>
                `
              )
              .join("")}</div>`
          : `<div class="empty">This artifact has no recorded upstream artifacts.</div>`
      }
    </section>
  `;
  document.getElementById("publish-artifact")?.addEventListener("click", async () => {
    const suggestion = `build/${artifact.output_name}`;
    const path = prompt("Project-relative publish path", suggestion);
    if (!path) {
      return;
    }
    const response = await postJSON("/api/publish", { ref, path });
    showFlash(`Published to ${response.path}`);
  });
  document.getElementById("copy-ref")?.addEventListener("click", async () => {
    await navigator.clipboard.writeText(ref);
    showFlash(`Copied ${ref}`);
  });
}

function bindRunButtons() {
  document.querySelectorAll("[data-run-pipeline]").forEach((button) => {
    button.addEventListener("click", async () => {
      await runPipeline(button.getAttribute("data-run-pipeline"));
    });
  });
}

async function runPipeline(name) {
  showFlash(`Running ${name}...`);
  const response = await postJSON("/api/run", { pipeline: name });
  await refreshStatus();
  if (response.error) {
    showFlash(`Run ${response.run.id} finished with error: ${response.error}`, true);
  } else {
    showFlash(`Run ${response.run.id} completed successfully.`);
  }
  location.hash = `#/run/${encodeURIComponent(response.run.id)}`;
}

function renderRunListItem(run) {
  return `
    <div class="list-item">
      <div class="meta-row">
        <strong>${escapeHTML(run.pipeline)}</strong>
        <span class="chip status-${escapeAttr(run.status)}">${escapeHTML(run.status)}</span>
      </div>
      <p class="muted code">${escapeHTML(run.id)}</p>
      <div class="run-actions">
        <span class="muted">${escapeHTML(formatDateTime(run.started_at))}</span>
        <a href="#/run/${encodeURIComponent(run.id)}">Open run</a>
      </div>
    </div>
  `;
}

async function fetchJSON(url) {
  const response = await fetch(url, {
    headers: { Accept: "application/json" }
  });
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload;
}

async function postJSON(url, body) {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json"
    },
    body: JSON.stringify(body)
  });
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  return payload;
}

function showFlash(message, isError = false) {
  els.flash.classList.remove("hidden", "error");
  if (isError) {
    els.flash.classList.add("error");
  }
  els.flash.textContent = message;
}

function clearFlash() {
  els.flash.classList.add("hidden");
  els.flash.classList.remove("error");
  els.flash.textContent = "";
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function escapeAttr(value) {
  return escapeHTML(value).replaceAll("`", "");
}

function formatDateTime(value) {
  if (!value) {
    return "-";
  }
  return new Date(value).toLocaleString();
}

function formatDuration(value) {
  if (!value) {
    return "0 ms";
  }
  if (value < 1000) {
    return `${value} ms`;
  }
  return `${(value / 1000).toFixed(2)} s`;
}

function formatBytes(value) {
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

const state = {
  days: 30,
  summary: null,
};

const els = {
  range: document.querySelector("#range"),
  scanButton: document.querySelector("#scanButton"),
  subtitle: document.querySelector("#subtitle"),
  totalTokens: document.querySelector("#totalTokens"),
  totalEvents: document.querySelector("#totalEvents"),
  inputTokens: document.querySelector("#inputTokens"),
  outputTokens: document.querySelector("#outputTokens"),
  cacheTokens: document.querySelector("#cacheTokens"),
  dailyChart: document.querySelector("#dailyChart"),
  toolList: document.querySelector("#toolList"),
  eventsBody: document.querySelector("#eventsBody"),
  scanList: document.querySelector("#scanList"),
  toast: document.querySelector("#toast"),
};

els.range.addEventListener("change", () => {
  state.days = Number(els.range.value);
  loadSummary();
});

els.scanButton.addEventListener("click", async () => {
  els.scanButton.disabled = true;
  try {
    const res = await fetch("/api/scan", { method: "POST" });
    const payload = await readJSON(res);
    showToast(`Scan finished: ${fmt(payload.events_inserted)} new events`);
    await loadSummary();
  } catch (err) {
    showToast(err.message || "Scan failed");
  } finally {
    els.scanButton.disabled = false;
  }
});

async function loadSummary() {
  try {
    const res = await fetch(`/api/summary?days=${state.days}`);
    state.summary = await readJSON(res);
    render(state.summary);
  } catch (err) {
    showToast(err.message || "Failed to load summary");
  }
}

async function readJSON(res) {
  const payload = await res.json();
  if (!res.ok) {
    throw new Error(payload.error || `HTTP ${res.status}`);
  }
  return payload;
}

function render(summary) {
  const totals = summary.totals || {};
  els.subtitle.textContent = `${summary.days} day window · updated ${new Date(summary.generated_at).toLocaleString()}`;
  els.totalTokens.textContent = fmt(totals.total_tokens);
  els.totalEvents.textContent = `${fmt(totals.events)} events`;
  els.inputTokens.textContent = fmt(totals.input_tokens);
  els.outputTokens.textContent = fmt(totals.output_tokens);
  els.cacheTokens.textContent = fmt((totals.cache_read_tokens || 0) + (totals.cache_write_tokens || 0));
  renderDaily(summary.daily || []);
  renderTools(summary.tools || []);
  renderEvents(summary.recent_events || []);
  renderScans(summary.scans || []);
}

function renderDaily(days) {
  els.dailyChart.innerHTML = "";
  els.dailyChart.style.setProperty("--days", Math.max(days.length, 1));
  if (!days.length) {
    els.dailyChart.innerHTML = `<div class="empty">No token events found in this range.</div>`;
    return;
  }
  const max = Math.max(...days.map((item) => item.total_tokens || 0), 1);
  for (const item of days) {
    const bar = document.createElement("div");
    bar.className = "bar";
    bar.tabIndex = 0;
    bar.style.height = `${Math.max(2, ((item.total_tokens || 0) / max) * 100)}%`;
    bar.dataset.label = `${item.date}: ${fmt(item.total_tokens)} tokens`;
    els.dailyChart.appendChild(bar);
  }
}

function renderTools(tools) {
  els.toolList.innerHTML = "";
  if (!tools.length) {
    els.toolList.innerHTML = `<div class="empty">No tools configured.</div>`;
    return;
  }
  for (const tool of tools) {
    const percent = Math.max(0, Math.min(100, tool.usage_percent || 0));
    const row = document.createElement("div");
    row.className = "tool";
    row.innerHTML = `
      <div class="tool-row">
        <strong>${escapeHTML(tool.display_name || tool.name)}</strong>
        <code>${fmt(tool.total_tokens)} tokens</code>
      </div>
      <div class="meter" aria-label="${escapeHTML(tool.name)} quota usage">
        <span style="--value:${percent}%"></span>
      </div>
      <div class="tool-row muted">
        <span>${tool.monthly_quota_tokens ? `${percent.toFixed(1)}% quota` : "No quota set"}</span>
        <span>${tool.monthly_cost_usd ? `$${money(tool.estimated_value_usd)} estimated value` : "$0 plan cost"}</span>
      </div>
    `;
    els.toolList.appendChild(row);
  }
}

function renderEvents(events) {
  els.eventsBody.innerHTML = "";
  if (!events.length) {
    els.eventsBody.innerHTML = `<tr><td colspan="7" class="muted">No events parsed yet.</td></tr>`;
    return;
  }
  for (const ev of events) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${new Date(ev.occurred_at).toLocaleString()}</td>
      <td>${escapeHTML(ev.tool_name)}</td>
      <td>${escapeHTML(ev.model || "unknown")}</td>
      <td>${escapeHTML(shorten(ev.project || "unknown", 54))}</td>
      <td class="num">${fmt(ev.input_tokens)}</td>
      <td class="num">${fmt(ev.output_tokens)}</td>
      <td class="num">${fmt(ev.total_tokens)}</td>
    `;
    els.eventsBody.appendChild(tr);
  }
}

function renderScans(scans) {
  els.scanList.innerHTML = "";
  if (!scans.length) {
    els.scanList.innerHTML = `<div class="empty">No scan has completed yet.</div>`;
    return;
  }
  for (const scan of scans) {
    const statusClass = scan.status === "ok" ? "status-ok" : scan.status === "error" ? "status-error" : "";
    const row = document.createElement("div");
    row.className = "scan";
    row.innerHTML = `
      <div class="scan-row">
        <strong class="${statusClass}">${escapeHTML(scan.status)}</strong>
        <span>${new Date(scan.started_at).toLocaleString()}</span>
      </div>
      <div class="scan-row muted">
        <span>${fmt(scan.files_seen)} files</span>
        <span>${fmt(scan.events_inserted)} new / ${fmt(scan.events_seen)} seen</span>
      </div>
      ${scan.error ? `<p class="status-error">${escapeHTML(shorten(scan.error, 160))}</p>` : ""}
    `;
    els.scanList.appendChild(row);
  }
}

function showToast(message) {
  els.toast.textContent = message;
  els.toast.classList.add("show");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => els.toast.classList.remove("show"), 3200);
}

function fmt(value) {
  return Number(value || 0).toLocaleString();
}

function money(value) {
  return Number(value || 0).toFixed(2);
}

function shorten(value, max) {
  if (value.length <= max) return value;
  return `${value.slice(0, Math.max(0, max - 1))}…`;
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

loadSummary();
window.setInterval(loadSummary, 60_000);


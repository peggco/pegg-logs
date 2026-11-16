const token = new URLSearchParams(location.search).get("token") || "";
const $ = (id) => document.getElementById(id);

const state = {
  page: 1,
  limit: 100,
  levels: ["DEBUG", "INFO", "WARN", "ERROR"],
  query: "",
  days: 7,
  from: "",
  to: "",
  total: 0,
  loading: false,
};

function api(path, params) {
  const url = new URL(path, location.origin);
  url.searchParams.set("token", token);
  if (params) {
    for (const [key, value] of params) url.searchParams.append(key, value);
  }
  return fetch(url, { headers: { "X-Logs-Token": token } }).then(async (res) => {
    if (!res.ok) {
      let message = res.statusText;
      try {
        const body = await res.json();
        if (body && body.error) message = body.error;
      } catch (_) {}
      throw new Error(message);
    }
    return res.json();
  });
}

function filterParams() {
  const params = new URLSearchParams();
  for (const level of state.levels) params.append("level", level);
  if (state.query) params.set("query", state.query);
  if (state.from) params.set("from", state.from);
  if (state.to) params.set("to", state.to);
  if (!state.from && !state.to) params.set("days", String(state.days));
  return params;
}

function pageParams() {
  const params = filterParams();
  params.set("limit", String(state.limit));
  params.set("page", String(state.page));
  params.set("sort", "desc");
  return params;
}

function escapeHTML(value) {
  return String(value == null ? "" : value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function fmtCount(value) {
  const n = Number(value || 0);
  return n.toLocaleString();
}

function fmtDate(value) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function setStatus(text, isError) {
  const el = $("status");
  el.textContent = text;
  el.classList.toggle("is-error", !!isError);
}

async function loadMeta() {
  try {
    const meta = await api("/api/meta");
    $("range-label").textContent =
      `${fmtCount(meta.total)} rows · ${meta.db_path || ""}` +
      (meta.last_timestamp ? ` · latest ${meta.last_timestamp}` : "");
  } catch (err) {
    $("range-label").textContent = err.message;
  }
}

async function loadStats() {
  const stats = await api("/api/stats", filterParams());
  $("card-total").textContent = fmtCount(stats.total);
  $("card-error").textContent = fmtCount(stats.levels?.ERROR || 0);
  $("card-warn").textContent = fmtCount(stats.levels?.WARN || 0);
  $("card-info").textContent = fmtCount(stats.levels?.INFO || 0);
  $("card-debug").textContent = fmtCount(stats.levels?.DEBUG || 0);
}

async function loadLogs() {
  const data = await api("/api/logs", pageParams());
  state.total = data.total;
  const rows = data.logs || [];
  const body = $("log-rows");
  const empty = $("empty");
  if (!rows.length) {
    body.innerHTML = "";
    empty.hidden = false;
  } else {
    empty.hidden = true;
    body.innerHTML = rows.map((row) => {
      return (
        "<tr>" +
        `<td class="time">${escapeHTML(fmtDate(row.timestamp))}</td>` +
        `<td><span class="level level-${escapeHTML(row.level)}">${escapeHTML(row.level)}</span></td>` +
        `<td class="message">${escapeHTML(row.message)}</td>` +
        "</tr>"
      );
    }).join("");
  }
  const pages = Math.max(1, Math.ceil(state.total / state.limit));
  $("page-label").textContent = `Page ${state.page} of ${pages}`;
  $("prev").disabled = state.page <= 1;
  $("next").disabled = state.page >= pages;
  $("table-note").textContent = `${fmtCount(state.total)} rows`;
  const params = pageParams();
  params.set("token", token);
  const query = params.toString();
  $("export-csv").href = `/api/export.csv?${query}`;
  $("export-json").href = `/api/export.json?${query}`;
}

async function refresh() {
  if (state.loading) return;
  state.loading = true;
  setStatus("Loading…");
  try {
    await Promise.all([loadMeta(), loadStats(), loadLogs()]);
    setStatus("Live");
  } catch (err) {
    setStatus(err.message, true);
  } finally {
    state.loading = false;
  }
}

function syncControls() {
  $("query").value = state.query;
  updateClearButton();
  $("days").value = String(state.days);
  $("from").value = state.from;
  $("to").value = state.to;
  $("limit").value = String(state.limit);
  document.querySelectorAll(".level-checks input").forEach((input) => {
    input.checked = state.levels.includes(input.value);
  });
}

function updateClearButton() {
  $("clear-query").hidden = $("query").value.length === 0;
}

function applyFilters() {
  state.query = $("query").value.trim();
  state.days = Number($("days").value) || 0;
  state.from = $("from").value;
  state.to = $("to").value;
  state.limit = Number($("limit").value) || 100;
  state.levels = Array.from(document.querySelectorAll(".level-checks input:checked")).map((input) => input.value);
  if (!state.levels.length) state.levels = ["DEBUG", "INFO", "WARN", "ERROR"];
  state.page = 1;
  refresh();
}

$("apply").addEventListener("click", applyFilters);
$("reset").addEventListener("click", () => {
  state.query = "";
  state.days = 7;
  state.from = "";
  state.to = "";
  state.limit = 100;
  state.levels = ["DEBUG", "INFO", "WARN", "ERROR"];
  state.page = 1;
  syncControls();
  refresh();
});
let searchTimer = 0;
$("query").addEventListener("input", () => {
  updateClearButton();
  window.clearTimeout(searchTimer);
  const value = $("query").value.trim();
  if (value === state.query) return;
  searchTimer = window.setTimeout(applyFilters, 300);
});
$("query").addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    window.clearTimeout(searchTimer);
    applyFilters();
  } else if (event.key === "Escape") {
    $("query").value = "";
    updateClearButton();
    if (state.query) applyFilters();
  }
});
$("clear-query").addEventListener("click", () => {
  $("query").value = "";
  updateClearButton();
  $("query").focus();
  if (state.query) applyFilters();
});
document.addEventListener("keydown", (event) => {
  if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey) return;
  const tag = (event.target && event.target.tagName) || "";
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
  event.preventDefault();
  $("query").focus();
});
$("prev").addEventListener("click", () => {
  if (state.page > 1) {
    state.page -= 1;
    refresh();
  }
});
$("next").addEventListener("click", () => {
  state.page += 1;
  refresh();
});
$("auto-refresh").addEventListener("change", () => {});

setInterval(() => {
  if ($("auto-refresh").checked && !document.hidden) refresh();
}, 5000);

syncControls();
refresh();

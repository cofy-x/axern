var state = {
  view: "overview",
  services: [],
  tunnels: [],
  quotas: [],
  quotaEvents: {},
  quotaEventErrors: {},
  quotaEventsLoading: "",
  admin: { retries: [], audit: [] },
  summary: null,
  reconcileHealth: { components: [] },
  quotaFilter: "constrained",
  quotaSearch: "",
  selectedService: "",
  selectedSession: "",
  selectedNamespace: "",
  pendingAdminAction: null,
  adminActionSubmitting: false,
  links: null,
  timer: null,
};

function $(id) {
  return document.getElementById(id);
}

function api(path) {
  return fetch(path).then(async (res) => {
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || res.statusText);
    return data;
  });
}

function apiPost(path, body) {
  return fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then(async (res) => {
    const data = await res.json();
    if (!res.ok) {
      const err = new Error(data.error || res.statusText);
      err.errorClass = data.error_class || "unknown";
      throw err;
    }
    return data;
  });
}

function setStatus(text, bad = false) {
  const el = $("status-line");
  el.textContent = text;
  el.style.color = bad ? "#aa2e25" : "#65707d";
}

function switchView(view) {
  state.view = view;
  document.querySelectorAll(".view").forEach((el) => el.classList.toggle("active", el.id === view));
  document.querySelectorAll(".nav").forEach((el) => el.classList.toggle("active", el.dataset.view === view));
  $("view-title").textContent = view[0].toUpperCase() + view.slice(1);
}

function pill(status) {
  const safe = status || "unknown";
  return `<span class="pill ${escapeHtml(safe)}">${escapeHtml(safe)}</span>`;
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[ch]));
}

function item(title, meta, status, onClick, active) {
  const btn = document.createElement("button");
  btn.className = "item" + (active ? " active" : "");
  btn.innerHTML = `<span class="item-main"><span class="id">${escapeHtml(title)}</span><span class="meta">${escapeHtml(meta || "")}</span></span>${pill(status)}`;
  if (onClick) btn.addEventListener("click", onClick);
  return btn;
}

function repairTargetLabel(value) {
  const targetType = value?.repair_target_type || value?.target_type || "";
  const targetID = value?.repair_target_id || value?.target_id || "";
  if (!targetType) return targetID;
  if (!targetID) return targetType;
  return `${targetType}/${targetID}`;
}

function renderList(target, rows, render, emptyText = "No data.") {
  const el = $(target);
  el.innerHTML = "";
  if (!rows.length) {
    el.classList.add("empty");
    el.textContent = emptyText;
    return;
  }
  el.classList.remove("empty");
  rows.forEach((row) => el.appendChild(render(row)));
}

function kv(label, value, copyValue) {
  const shown = value === undefined || value === null || value === "" ? "-" : value;
  const copy = copyValue;
  return `<div class="kv"><span>${escapeHtml(label)}</span><span>${escapeHtml(shown)}${copy ? `<button class="copy" data-copy="${escapeHtml(copy)}">copy</button>` : ""}</span></div>`;
}

function wireCopies(root = document) {
  root.querySelectorAll("[data-copy]").forEach((btn) => {
    btn.addEventListener("click", () => navigator.clipboard?.writeText(btn.dataset.copy));
  });
}

function setSectionVisible(id, visible) {
  const el = $(id);
  if (el) el.hidden = !visible;
}

function formatCPU(cpuMilli) {
  if (cpuMilli < 1000 && cpuMilli > -1000) return `${cpuMilli}m`;
  return `${Number(cpuMilli / 1000).toLocaleString(undefined, { maximumFractionDigits: 3 })} CPU`;
}

function formatMemory(bytes) {
  const units = [
    ["TiB", 1024 ** 4],
    ["GiB", 1024 ** 3],
    ["MiB", 1024 ** 2],
    ["KiB", 1024],
  ];
  for (const [suffix, size] of units) {
    if (bytes && bytes % size === 0) return `${bytes / size}${suffix}`;
  }
  return `${bytes || 0}B`;
}

function formatTimestamp(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

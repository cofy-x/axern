function renderQuotas() {
  const sorted = sortedQuotas(state.quotas);
  syncQuotaFilterButtons(sorted);
  const quotas = searchedQuotas(filteredQuotas(sorted));
  $("quota-layout")?.classList.toggle("empty", quotas.length === 0);
  if (!quotas.find((quota) => quotaNamespace(quota) === state.selectedNamespace)) {
    state.selectedNamespace = quotas[0] ? quotaNamespace(quotas[0]) : "";
  }
  renderQuotaGrid("quotas-grid", quotas, "No namespaces match this filter.", selectQuota);
  const detail = $("quota-detail");
  if (!quotas.length) {
    if (detail) detail.hidden = true;
    return;
  }
  if (detail) detail.hidden = false;
  renderQuotaDetail(quotas.find((quota) => quotaNamespace(quota) === state.selectedNamespace));
  if (state.view === "quotas") {
    ensureQuotaEventsLoaded(state.selectedNamespace);
  }
}

function renderQuotaGrid(target, quotas, emptyText = "No data.", onSelect) {
  const el = $(target);
  el.innerHTML = "";
  if (!quotas.length) {
    el.classList.add("empty");
    el.textContent = emptyText;
    return;
  }
  el.classList.remove("empty");
  quotas.forEach((quota) => el.appendChild(quotaCard(quota, onSelect)));
}

function quotaCard(quota, onSelect) {
  const card = document.createElement(onSelect ? "button" : "article");
  if (onSelect) {
    card.type = "button";
    card.addEventListener("click", () => onSelect(quotaNamespace(quota)));
  }
  const active = onSelect && state.selectedNamespace === quotaNamespace(quota) ? " active" : "";
  const compact = isLowSignalQuota(quota) ? " compact" : "";
  card.className = `quota-card ${quotaPressureStatus(quota)}${active}${compact}`;
  const body = isLowSignalQuota(quota)
    ? [
      `<div class="meta">${escapeHtml(compactQuotaResource("CPU", quota.reserved_cpu_milli, quota.cpu_milli_limit, quota.cpu_usage_percent, formatCPU))}</div>`,
      `<div class="meta">${escapeHtml(compactQuotaResource("Memory", quota.reserved_memory_bytes, quota.memory_bytes_limit, quota.memory_usage_percent, formatMemory))}</div>`,
    ]
    : [
      quotaResource("CPU", quota.reserved_cpu_milli, quota.cpu_milli_limit, quota.available_cpu_milli, quota.cpu_usage_percent, formatCPU),
      quotaResource("Memory", quota.reserved_memory_bytes, quota.memory_bytes_limit, quota.available_memory_bytes, quota.memory_usage_percent, formatMemory),
    ];
  card.innerHTML = [
    `<header><strong>${escapeHtml(quotaNamespace(quota))}</strong><span>v${escapeHtml(quota.version || 0)}</span></header>`,
    ...body,
    quota.updated_at ? `<div class="meta">updated ${escapeHtml(formatTimestamp(quota.updated_at))}</div>` : "",
  ].join("");
  return card;
}

function selectQuota(namespace) {
  state.selectedNamespace = namespace || "default";
  switchView("quotas");
  renderQuotas();
  refreshSelectedQuotaEvents();
}

function renderQuotaDetail(quota) {
  const el = $("quota-detail");
  if (!quota) {
    el.classList.add("empty");
    el.textContent = "Select a namespace quota.";
    return;
  }
  const blockedServices = state.services.filter((svc) => (svc.namespace || "default") === quotaNamespace(quota) && svc.admission_summary);
  const blockers = namespaceDeleteBlockers(quota, state.services);
  const namespace = quotaNamespace(quota);
  el.classList.remove("empty");
  el.innerHTML = [
    `<h2>Quota Detail</h2>`,
    kv("namespace", namespace, namespace),
    kv("version", quota.version || 0),
    kv("pressure", quotaPressureStatus(quota)),
    quotaDetailResource("CPU", quota.reserved_cpu_milli, quota.cpu_milli_limit, quota.available_cpu_milli, quota.cpu_usage_percent, formatCPU),
    quotaDetailResource("Memory", quota.reserved_memory_bytes, quota.memory_bytes_limit, quota.available_memory_bytes, quota.memory_usage_percent, formatMemory),
    quota.updated_at ? kv("updated", formatTimestamp(quota.updated_at)) : "",
    `<h3>Namespace Lifecycle</h3>`,
    blockers.length
      ? `<div class="blocker-list">${blockers.map((blocker) => `<div class="blocker"><strong>${escapeHtml(blocker.label)}</strong><span>${escapeHtml(blocker.detail)}</span></div>`).join("")}</div>`
      : `<div class="empty">No known quota or service blockers.</div>`,
    `<div class="meta">delete also requires no live environments, active runs, active reservations, live services, or secrets</div>`,
    `<h3>Admission Signals</h3>`,
    blockedServices.length
      ? blockedServices.map((svc) => `<div class="event"><strong>${escapeHtml(svc.id)} ${pill("admission-blocked")}</strong><p>${escapeHtml(svc.admission_summary)}</p></div>`).join("")
      : `<div class="empty">No admission-blocked services in this namespace.</div>`,
    `<h3>Recent Quota Events</h3>`,
    renderQuotaEvents(namespace),
  ].join("");
  wireCopies(el);
}

function renderQuotaEvents(namespace) {
  if (state.quotaEventsLoading === namespace) {
    return `<div class="empty">Loading quota events.</div>`;
  }
  if (state.quotaEventErrors[namespace]) {
    return `<div class="empty">${escapeHtml(state.quotaEventErrors[namespace])}</div>`;
  }
  const events = state.quotaEvents[namespace] || [];
  if (!events.length) {
    return `<div class="empty">No recent quota admission events.</div>`;
  }
  return events.map((event) => [
    `<div class="event quota-event">`,
    `<strong>${escapeHtml(quotaEventTitle(event))} ${pill(event.reason || event.type || "quota-event")}</strong>`,
    `<span class="meta">${escapeHtml(formatTimestamp(event.created_at || ""))}</span>`,
    `<p>${escapeHtml(event.message || quotaEventResourceSummary(event))}</p>`,
    `</div>`,
  ].join("")).join("");
}

function quotaEventTitle(event) {
  const workload = [event.workload_type, event.workload_id].filter(Boolean).join(" ");
  return workload || event.type || "quota event";
}

function quotaEventResourceSummary(event) {
  const cpu = `${formatCPU(event.requested_cpu_milli || 0)} requested, ${formatCPU(event.reserved_cpu_milli || 0)} reserved`;
  const memory = `${formatMemory(event.requested_memory_bytes || 0)} requested, ${formatMemory(event.reserved_memory_bytes || 0)} reserved`;
  return `CPU ${cpu}; Memory ${memory}`;
}

function ensureQuotaEventsLoaded(namespace) {
  if (!namespace || state.quotaEvents[namespace] || state.quotaEventErrors[namespace] || state.quotaEventsLoading === namespace) {
    return;
  }
  loadQuotaEvents(namespace);
}

function refreshSelectedQuotaEvents() {
  if (state.view !== "quotas" || !state.selectedNamespace) {
    return;
  }
  loadQuotaEvents(state.selectedNamespace, true);
}

async function loadQuotaEvents(namespace, force = false) {
  if (!namespace || state.quotaEventsLoading === namespace || (!force && state.quotaEvents[namespace])) {
    return;
  }
  state.quotaEventsLoading = namespace;
  renderQuotas();
  try {
    const data = await api(`/api/quotas/${encodeURIComponent(namespace)}/events?limit=20`);
    state.quotaEvents[namespace] = data.events || [];
    delete state.quotaEventErrors[namespace];
  } catch (err) {
    state.quotaEventErrors[namespace] = err.message;
  } finally {
    if (state.quotaEventsLoading === namespace) {
      state.quotaEventsLoading = "";
    }
    renderQuotas();
  }
}

function namespaceDeleteBlockers(quota, services) {
  const namespace = quotaNamespace(quota);
  const blockers = [];
  if ((quota.reserved_cpu_milli || 0) > 0 || (quota.reserved_memory_bytes || 0) > 0) {
    blockers.push({
      label: "active reservations",
      detail: `${formatCPU(quota.reserved_cpu_milli || 0)} CPU, ${formatMemory(quota.reserved_memory_bytes || 0)} memory reserved`,
    });
  }
  const liveServices = services.filter((svc) => (svc.namespace || "default") === namespace && svc.status !== "deleted");
  if (liveServices.length) {
    blockers.push({
      label: "live services",
      detail: `${liveServices.length} service${liveServices.length === 1 ? "" : "s"}`,
    });
  }
  return blockers;
}

function quotaDetailResource(label, reserved, limit, available, usagePercent, format) {
  const constrained = limit !== undefined && limit !== null;
  return [
    `<h3>${escapeHtml(label)}</h3>`,
    kv("reserved", format(reserved || 0)),
    kv("limit", constrained ? format(limit) : "-"),
    kv("available", constrained ? format(available || 0) : "-"),
    kv("usage", constrained ? `${quotaUsagePercent(reserved, limit, usagePercent)}%` : "-"),
  ].join("");
}

function quotaResource(label, reserved, limit, available, usagePercent, format) {
  const constrained = limit !== undefined && limit !== null;
  const pct = constrained ? quotaUsagePercent(reserved, limit, usagePercent) : 0;
  const availableLabel = constrained ? format(available || 0) : "-";
  const usageLabel = constrained ? `${pct}% used` : "-";
  return [
    `<div class="quota-resource">`,
    `<div class="quota-row"><span>${escapeHtml(label)}</span><strong>${escapeHtml(format(reserved || 0))} / ${escapeHtml(constrained ? format(limit) : "-")}</strong></div>`,
    `<div class="quota-bar ${quotaBarClass(constrained, pct)}"><span style="width:${pct}%"></span></div>`,
    `<div class="meta">${escapeHtml(usageLabel)} · available ${escapeHtml(availableLabel)}</div>`,
    `</div>`,
  ].join("");
}

function compactQuotaResource(label, reserved, limit, usagePercent, format) {
  const constrained = limit !== undefined && limit !== null;
  const pct = constrained ? ` ${quotaUsagePercent(reserved, limit, usagePercent)}%` : "";
  return `${label} ${format(reserved || 0)} / ${constrained ? format(limit) + pct : "-"}`;
}

function quotaUsagePercent(reserved, limit, provided) {
  if (Number.isFinite(provided)) return Math.min(100, Math.max(0, Math.round(provided)));
  if (!limit || limit <= 0) return 0;
  return Math.min(100, Math.max(0, Math.round(((reserved || 0) / limit) * 100)));
}

function quotaUsageLabel(value) {
  return Number.isFinite(value) ? `${Math.round(value)}%` : "-";
}

function quotaPressureScore(quota) {
  return Math.max(
    Number.isFinite(quota.cpu_usage_percent) ? quota.cpu_usage_percent : 0,
    Number.isFinite(quota.memory_usage_percent) ? quota.memory_usage_percent : 0,
  );
}

function quotaNamespace(quota) {
  return quota.namespace || "default";
}

function hasReservedQuota(quota) {
  return (quota.reserved_cpu_milli || 0) > 0 || (quota.reserved_memory_bytes || 0) > 0;
}

function isLowSignalQuota(quota) {
  return !hasQuotaPressure(quota) && !hasConstrainedQuota(quota) && !hasReservedQuota(quota);
}

function hasQuotaPressure(quota) {
  return quotaPressureScore(quota) >= 80;
}

function hasConstrainedQuota(quota) {
  return (quota.cpu_milli_limit !== undefined && quota.cpu_milli_limit !== null)
    || (quota.memory_bytes_limit !== undefined && quota.memory_bytes_limit !== null);
}

function sortedQuotas(quotas) {
  return [...quotas].sort((a, b) => {
    const pressure = quotaPressureScore(b) - quotaPressureScore(a);
    if (pressure !== 0) return pressure;
    const constrained = Number(hasConstrainedQuota(b)) - Number(hasConstrainedQuota(a));
    if (constrained !== 0) return constrained;
    const reserved = Number(hasReservedQuota(b)) - Number(hasReservedQuota(a));
    if (reserved !== 0) return reserved;
    const updated = quotaUpdatedTime(b) - quotaUpdatedTime(a);
    if (updated !== 0) return updated;
    return String(a.namespace || "default").localeCompare(String(b.namespace || "default"));
  });
}

function filteredQuotas(quotas) {
  switch (state.quotaFilter) {
    case "constrained":
      return quotas.filter(hasConstrainedQuota);
    case "pressure":
      return quotas.filter(hasQuotaPressure);
    default:
      return quotas;
  }
}

function searchedQuotas(quotas) {
  const query = state.quotaSearch.trim().toLowerCase();
  if (!query) return quotas;
  return quotas.filter((quota) => quotaNamespace(quota).toLowerCase().includes(query));
}

function quotaUpdatedTime(quota) {
  const timestamp = Date.parse(quota.updated_at || "");
  return Number.isFinite(timestamp) ? timestamp : 0;
}

function syncQuotaFilterButtons(quotas = state.quotas) {
  const counts = {
    all: quotas.length,
    constrained: quotas.filter(hasConstrainedQuota).length,
    pressure: quotas.filter(hasQuotaPressure).length,
  };
  document.querySelectorAll("[data-quota-filter]").forEach((btn) => {
    btn.classList.toggle("active", btn.dataset.quotaFilter === state.quotaFilter);
    const label = btn.dataset.quotaLabel || btn.dataset.quotaFilter || "";
    btn.textContent = `${label} ${counts[btn.dataset.quotaFilter] ?? 0}`;
  });
}

function quotaPressureStatus(quota) {
  const score = quotaPressureScore(quota);
  if (score >= 95) return "critical";
  if (score >= 80) return "warning";
  return "normal";
}

function quotaBarClass(constrained, pct) {
  if (!constrained) return "unlimited";
  if (pct >= 95) return "critical";
  if (pct >= 80) return "warning";
  return "";
}

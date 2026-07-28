function serviceItem(svc) {
  const detail = svc.admission_summary
    ? `${svc.namespace || "default"} ${svc.admission_summary}`
    : `${svc.namespace || "default"} replicas ${svc.ready_replicas}/${svc.replicas}`;
  return item(
    svc.id,
    detail,
    svc.status,
    () => selectService(svc.id),
    state.selectedService === svc.id,
  );
}

function renderServices() {
  renderList("services-list", state.services, serviceItem);
}

async function selectService(id) {
  state.selectedService = id;
  $("service-search").value = id;
  switchView("services");
  try {
    const detail = await api(`/api/services/${encodeURIComponent(id)}`);
    renderServiceDetail(detail);
    renderServiceEvents(detail.events || []);
    renderServices();
  } catch (err) {
    $("service-detail").textContent = err.message;
    $("service-detail").classList.add("empty");
  }
}

function renderServiceDetail(detail) {
  const svc = detail.service || {};
  const el = $("service-detail");
  el.classList.remove("empty");
  el.innerHTML = [
    `<h2>Service</h2>`,
    kv("id", svc.id, svc.id),
    kv("namespace", svc.namespace),
    kv("status", svc.status),
    kv("replicas", `${svc.ready_replicas || 0}/${svc.replicas || 0}`),
    kv("runtime class", svc.runtime_class),
    serviceResourceSection(svc.resources),
    kv("message", svc.message),
    kv("rollout", svc.rollout_phase),
    kv("diagnostic", svc.diagnostic_code),
    kv("diagnostic message", svc.diagnostic_message),
    kv("admission", svc.admission_summary),
    serviceLifecycleRetrySection(detail.replicas || []),
    renderGatewayRouteForm(),
    `<h3>Replicas</h3>`,
    ...(detail.replicas || []).map((rep) => `<div class="event"><strong>${escapeHtml(rep.id)} ${pill(rep.status)}</strong><span class="meta">node ${escapeHtml(rep.node_id || "-")} ready=${rep.ready} outdated=${rep.outdated}${replicaRetryMeta(rep)}</span></div>`),
  ].join("");
  wireCopies(el);
  wireManualGatewayRoute(svc);
}

function serviceLifecycleRetrySection(replicas) {
  const retries = replicas.filter((rep) => rep.lifecycle_retry);
  if (retries.length === 0) return "";
  return [
    `<h3>Lifecycle Retry</h3>`,
    ...retries.map((rep) => {
      const retry = rep.lifecycle_retry || {};
      return `<div class="event retry-event"><strong>${escapeHtml(rep.id)} ${pill(retry.reason || "retry", "warning")}</strong><span class="meta">${escapeHtml(replicaRetryLabel(retry))}</span></div>`;
    }),
  ].join("");
}

function replicaRetryMeta(rep) {
  const retry = rep.lifecycle_retry;
  if (!retry) return "";
  return ` · ${escapeHtml(replicaRetryLabel(retry))}`;
}

function replicaRetryLabel(retry) {
  const parts = [`retry=${retry.reason || "pending"}`, `attempts=${retry.attempts || 0}`];
  if (retry.next_run_at) parts.push(`next=${retry.next_run_at}`);
  if (retry.last_error) parts.push(retry.last_error);
  return parts.join(" · ");
}

function serviceResourceSection(resources) {
  if (!resources) return "";
  return [
    `<h3>Resources</h3>`,
    kv("request cpu", resourceCPU(resources.requests?.cpu_milli)),
    kv("request memory", resourceMemory(resources.requests?.memory_bytes)),
    kv("limit cpu", resourceCPU(resources.limits?.cpu_milli)),
    kv("limit memory", resourceMemory(resources.limits?.memory_bytes)),
  ].join("");
}

function resourceCPU(cpuMilli) {
  return Number.isFinite(cpuMilli) && cpuMilli > 0 ? formatCPU(cpuMilli) : "unset";
}

function resourceMemory(memoryBytes) {
  return Number.isFinite(memoryBytes) && memoryBytes > 0 ? formatMemory(memoryBytes) : "unset";
}

function renderGatewayRouteForm() {
  const hasGateway = Boolean(state.links?.links?.find((link) => link.kind === "gateway"));
  if (!hasGateway) {
    return `<h3>Gateway route</h3><div class="empty">Gateway HTTP target is not configured in the current context.</div>`;
  }
  return [
    `<h3>Gateway route</h3>`,
    `<div class="manual-route">`,
    `<div class="manual-route-form">`,
    `<input id="manual-gateway-port" placeholder="port or name, e.g. 80" />`,
    `<button id="open-manual-gateway-route">Open</button>`,
    `</div>`,
    `<div class="route-preview">`,
    `<code id="manual-gateway-url"></code>`,
    `<button id="copy-manual-gateway-route" class="copy" disabled>copy</button>`,
    `</div>`,
    `</div>`,
  ].join("");
}

function wireManualGatewayRoute(service) {
  const button = $("open-manual-gateway-route");
  const copyButton = $("copy-manual-gateway-route");
  const input = $("manual-gateway-port");
  const preview = $("manual-gateway-url");
  const gateway = state.links?.links?.find((link) => link.kind === "gateway")?.url;
  if (!button || !input || !service?.id || !gateway) return;
  const routeURL = () => {
    const port = input.value.trim();
    if (!port) return "";
    const base = gateway.split("/dashboard")[0].replace(/\/$/, "");
    const namespace = encodeURIComponent(service.namespace || "default");
    const serviceID = encodeURIComponent(service.id);
    const routePort = encodeURIComponent(port);
    return `${base}/svc/${namespace}/${serviceID}/${routePort}/`;
  };
  const updatePreview = () => {
    const url = routeURL();
    if (preview) preview.textContent = url || "Enter a port to preview the gateway URL.";
    if (copyButton) {
      copyButton.disabled = !url;
      copyButton.dataset.copy = url;
    }
  };
  updatePreview();
  input.addEventListener("input", updatePreview);
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      const url = routeURL();
      if (url) window.open(url, "_blank", "noreferrer");
    }
  });
  button.addEventListener("click", () => {
    const url = routeURL();
    if (url) window.open(url, "_blank", "noreferrer");
  });
  copyButton?.addEventListener("click", () => {
    const url = routeURL();
    if (url) navigator.clipboard?.writeText(url);
  });
}

function renderServiceEvents(events) {
  const el = $("service-events");
  el.classList.remove("empty");
  if (!events.length) {
    el.classList.add("empty");
    el.textContent = "No service events.";
    return;
  }
  el.innerHTML = events.map((ev) => `<div class="event"><strong>${escapeHtml(ev.type)}</strong><span class="meta">${escapeHtml(ev.created_at)} ${escapeHtml(ev.diagnostic_code || "")}</span><p>${escapeHtml(ev.message || "")}</p></div>`).join("");
}

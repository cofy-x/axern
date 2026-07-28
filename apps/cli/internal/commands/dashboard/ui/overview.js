function renderSummary(summary) {
  $("metric-services").textContent = summary.services?.total ?? "-";
  $("metric-ready").textContent = summary.services?.ready ?? "-";
  $("metric-admission").textContent = summary.services?.admission_blocked ?? "-";
  $("metric-cpu-pressure").textContent = summary.quotas?.cpu_pressure ?? "-";
  $("metric-memory-pressure").textContent = summary.quotas?.memory_pressure ?? "-";
  $("metric-tunnels").textContent = summary.tunnels?.total ?? "-";
  $("metric-reliability").textContent = summary.reliability?.status || "-";
  $("metric-admin-retries").textContent = summary.reliability?.allocation_lifecycle_retries ?? "-";
  $("metric-reconcile-failures").textContent = reconcileFailureCount();
  renderReliability(summary.reliability || {});
  renderReconcileHealth();
  renderAdmissionBlockedServices();
  renderQuotaPressure();
  renderList("overview-services", state.services.slice(0, 8), serviceItem);
  renderList("overview-tunnels", state.tunnels.slice(0, 8), tunnelItem);
  renderQuotaGrid("overview-quotas", sortedQuotas(state.quotas).filter((quota) => hasConstrainedQuota(quota) || hasReservedQuota(quota)).slice(0, 4), "No constrained quota usage.", selectQuota);
}

function renderReliability(reliability) {
  const rows = [
    ...(reliability.issues || []).map((issue) => ({ kind: "issue", issue })),
    ...(reliability.signals || []).map((signal) => ({ kind: "signal", signal })),
  ];
  setSectionVisible("overview-reliability-band", rows.length > 0);
  renderList("overview-reliability", rows, reliabilityItem, "No reliability signals.");
}

function reliabilityItem(row) {
  if (row.kind === "issue") return reliabilityIssueItem(row.issue);
  return reliabilitySignalItem(row.signal);
}

function reliabilityIssueItem(issue) {
  const target = repairTargetLabel(issue);
  const action = String(issue.repair_action || "repair").replaceAll("_", " ");
  const meta = [target, action, issue.detail].filter(Boolean).join(" · ");
  return item(
    String(issue.code || "consistency_issue").replaceAll("_", " "),
    meta,
    issue.severity || "degraded",
    undefined,
    false,
  );
}

function reliabilitySignalItem(signal) {
  return item(
    String(signal.code || "reliability").replaceAll("_", " "),
    signal.message || "",
    "degraded",
    undefined,
    false,
  );
}

function reconcileFailureCount() {
  return (state.reconcileHealth?.components || []).filter((component) => (component.consecutive_failures || 0) > 0).length;
}

function renderReconcileHealth() {
  const unhealthy = (state.reconcileHealth?.components || [])
    .filter((component) => component.running || (component.consecutive_failures || 0) > 0)
    .sort((a, b) => (b.consecutive_failures || 0) - (a.consecutive_failures || 0));
  setSectionVisible("overview-reconcile-health-band", unhealthy.length > 0);
  renderList("overview-reconcile-health", unhealthy, reconcileHealthItem, "No reconcile failures.");
}

function reconcileHealthItem(component) {
  const failures = component.consecutive_failures || 0;
  const status = failures > 0 ? "failed" : "running";
  const detail = failures > 0
    ? `${failures} consecutive failure${failures === 1 ? "" : "s"} · ${component.last_error || "unknown error"}`
    : "running";
  return item(component.component || "unknown", detail, status, undefined, false);
}

function renderAdmissionBlockedServices() {
  const blocked = state.services.filter((svc) => svc.admission_summary);
  setSectionVisible("overview-admission-band", blocked.length > 0);
  renderList("overview-admission", blocked.slice(0, 6), admissionBlockedServiceItem, "No admission blocks.");
}

function admissionBlockedServiceItem(svc) {
  return item(
    svc.id,
    `${svc.namespace || "default"} ${svc.admission_summary}`,
    "admission-blocked",
    () => selectService(svc.id),
    state.selectedService === svc.id,
  );
}

function renderQuotaPressure() {
  const pressured = sortedQuotas(state.quotas)
    .filter(hasQuotaPressure)
    .slice(0, 5);
  setSectionVisible("overview-quota-pressure-band", pressured.length > 0);
  renderList("overview-quota-pressure", pressured, quotaPressureItem, "No constrained quota usage.");
}

function quotaPressureItem(quota) {
  return item(
    quota.namespace || "default",
    `CPU ${quotaUsageLabel(quota.cpu_usage_percent)} · Memory ${quotaUsageLabel(quota.memory_usage_percent)}`,
    quotaPressureStatus(quota),
    () => selectQuota(quota.namespace || "default"),
    false,
  );
}

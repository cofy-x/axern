function renderAdmin() {
  const retries = state.admin?.retries || [];
  const audit = state.admin?.audit || [];
  $("metric-admin-retries").textContent = state.summary?.reliability?.allocation_lifecycle_retries ?? retries.length;
  $("admin-retry-count").textContent = retries.length ? `${retries.length} items` : "";
  $("admin-audit-count").textContent = audit.length ? `${audit.length} recent` : "";
  renderList("admin-retries", retries, adminRetryItem, "No allocation lifecycle retries.");
  renderAdminAudit(audit);
}

function adminRetryItem(retry) {
  const row = document.createElement("div");
  row.className = "admin-retry-row";
  const reason = retry.reason || "unknown";
  const owner = retryOwnerLabel(retry);
  const meta = [
    reason,
    owner,
    retry.node_id ? `node ${retry.node_id}` : "",
    retry.reconcile_attempts ? `${retry.reconcile_attempts} retries` : "",
  ].filter(Boolean).join(" · ");
  const main = document.createElement("button");
  main.className = "item admin-retry-main";
  main.innerHTML = `<span class="item-main"><span class="id">${escapeHtml(retry.allocation_id || "-")}</span><span class="meta">${escapeHtml(meta)}</span></span>${pill(retry.due ? "due" : "queued")}`;
  main.addEventListener("click", () => navigator.clipboard?.writeText(retry.allocation_id || ""));
  row.appendChild(main);

  const actions = document.createElement("div");
  actions.className = "admin-actions";
  actions.appendChild(adminActionButton("force", retry, {
    disabled: Boolean(retry.due),
    disabledReason: "already due",
  }));
  if (retry.reason === "create") {
    actions.appendChild(adminActionButton("fail", retry));
  }
  actions.appendChild(adminActionButton("clear", retry, {
    disabled: !retry.clearable,
    disabledReason: retry.clear_blocked_reason || "requires terminal cleanup proof",
  }));
  row.appendChild(actions);
  return row;
}

function retryOwnerLabel(retry) {
  if (retry.owner_type && retry.owner_id) return `${retry.owner_type}/${retry.owner_id}`;
  return retry.owner_id || retry.owner_type || "";
}

function adminActionButton(action, retry, options = {}) {
  const btn = document.createElement("button");
  btn.className = "admin-action-button";
  btn.textContent = action;
  btn.disabled = Boolean(options.disabled);
  if (options.disabledReason) btn.title = options.disabledReason;
  btn.addEventListener("click", () => openAdminAction(action, retry));
  return btn;
}

function renderAdminAudit(events) {
  const el = $("admin-audit");
  el.innerHTML = "";
  if (!events.length) {
    el.classList.add("empty");
    el.textContent = "No admin audit events.";
    return;
  }
  el.classList.remove("empty");
  el.innerHTML = events.map((ev) => [
    `<div class="event admin-audit-event">`,
    `<strong>${escapeHtml(adminOperationLabel(ev.operation))}</strong>`,
    `<span class="meta">${escapeHtml(formatTimestamp(ev.created_at))} · ${escapeHtml(ev.target_type || "-")} ${escapeHtml(ev.target_id || "")}</span>`,
    `<p>${escapeHtml(ev.operator_reason || "")}</p>`,
    `</div>`,
  ].join("")).join("");
}

function adminOperationLabel(value) {
  return String(value || "unknown").replaceAll("_", " ");
}

function openAdminAction(action, retry) {
  state.pendingAdminAction = { action, retry };
  state.adminActionSubmitting = false;
  $("admin-action-title").textContent = `${action} allocation lifecycle retry`;
  $("admin-action-impact").textContent = adminActionImpact(action);
  $("admin-action-object").innerHTML = [
    kv("allocation", retry.allocation_id, retry.allocation_id),
    kv("owner", retryOwnerLabel(retry)),
    kv("reason", retry.reason || "-"),
    kv("node", retry.node_id || "-"),
    kv("attempts", retry.reconcile_attempts || 0),
    retry.last_error ? kv("last error", retry.last_error) : "",
  ].join("");
  $("admin-action-reason").value = "";
  $("admin-action-error").textContent = "";
  $("admin-action-error").className = "admin-action-error";
  $("admin-action-close").disabled = false;
  $("admin-action-submit").disabled = true;
  $("admin-action-submit").textContent = action;
  $("admin-action-modal").hidden = false;
  wireCopies($("admin-action-object"));
  $("admin-action-reason").focus();
}

function adminActionImpact(action) {
  switch (action) {
    case "force":
      return "Schedules the existing retry immediately.";
    case "fail":
      return "Marks the owning run or service path failed and releases reservation state.";
    case "clear":
      return "Removes stale retry intent for an already-terminal allocation.";
    default:
      return "";
  }
}

function closeAdminAction() {
  if (state.adminActionSubmitting) return;
  state.pendingAdminAction = null;
  $("admin-action-modal").hidden = true;
}

async function submitAdminAction() {
  const pending = state.pendingAdminAction;
  if (!pending) return;
  const operatorReason = $("admin-action-reason").value.trim();
  if (!operatorReason) return;
  const submit = $("admin-action-submit");
  const close = $("admin-action-close");
  state.adminActionSubmitting = true;
  close.disabled = true;
  submit.disabled = true;
  submit.textContent = "Submitting";
  $("admin-action-error").textContent = "";
  try {
    const body = {
      reason: pending.action === "fail" ? "create" : pending.retry.reason,
      operator_reason: operatorReason,
    };
    await apiPost(`/api/admin/allocation-retries/${encodeURIComponent(pending.retry.allocation_id)}/${pending.action}`, body);
    const [summary, health, admin] = await Promise.all([
      api("/api/summary"),
      api("/api/reconcile-health"),
      api("/api/admin"),
    ]);
    state.summary = summary || null;
    state.reconcileHealth = health || { components: [] };
    state.admin = admin || { retries: [], audit: [] };
    renderSummary(summary);
    renderAdmin();
    state.adminActionSubmitting = false;
    close.disabled = false;
    closeAdminAction();
    setStatus(`Updated ${new Date().toLocaleTimeString()}`);
  } catch (err) {
    state.adminActionSubmitting = false;
    close.disabled = false;
    const el = $("admin-action-error");
    el.className = `admin-action-error ${adminErrorClass(err.errorClass)}`;
    el.textContent = err.message;
    submit.disabled = false;
    submit.textContent = pending.action;
  }
}

function adminErrorClass(value) {
  const safe = String(value || "unknown");
  switch (safe) {
    case "not-found":
    case "failed-precondition":
    case "invalid-argument":
    case "unavailable":
    case "unknown":
      return safe;
    default:
      return "unknown";
  }
}

function wireAdminActions() {
  $("admin-action-close").addEventListener("click", closeAdminAction);
  $("admin-action-submit").addEventListener("click", submitAdminAction);
  $("admin-action-reason").addEventListener("input", () => {
    $("admin-action-submit").disabled = $("admin-action-reason").value.trim() === "";
  });
}

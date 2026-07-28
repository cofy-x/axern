function tunnelItem(tun) {
  return item(
    tun.session_id,
    `${tun.allocation_id || "-"} ${tun.relay_id || ""}`,
    tun.status,
    () => selectTunnel(tun.session_id),
    state.selectedSession === tun.session_id,
  );
}

function renderTunnels() {
  renderList("tunnels-list", state.tunnels, tunnelItem);
}

async function selectTunnel(id) {
  state.selectedSession = id;
  $("session-search").value = id;
  switchView("tunnels");
  try {
    const detail = await api(`/api/tunnels/${encodeURIComponent(id)}`);
    const doctor = await api(`/api/tunnel-doctor?session_id=${encodeURIComponent(id)}`);
    renderTunnelDetail(detail, doctor);
    renderTunnelEvents(detail.events || []);
    renderTunnels();
  } catch (err) {
    $("tunnel-detail").textContent = err.message;
    $("tunnel-detail").classList.add("empty");
  }
}

function renderTunnelDetail(detail, doctor) {
  const tun = detail.session || {};
  const el = $("tunnel-detail");
  el.classList.remove("empty");
  el.innerHTML = [
    `<h2>Tunnel</h2>`,
    kv("session", tun.session_id, tun.session_id),
    kv("allocation", tun.allocation_id, tun.allocation_id),
    kv("node", tun.node_id),
    kv("status", tun.status),
    kv("relay", tun.relay_id),
    kv("bound", tun.bound_addr),
    kv("client target", tun.client_edge_target),
    kv("node target", tun.node_edge_target),
    kv("bytes", `${tun.bytes_in || 0} in / ${tun.bytes_out || 0} out`),
    `<h3>Doctor</h3>`,
    kv("recommendation", doctor.recommendation),
    ...(doctor.problems || []).map((p) => `<div class="event"><strong>problem</strong><p>${escapeHtml(p)}</p></div>`),
    ...(doctor.checks || []).map((p) => `<div class="event"><strong>ok</strong><p>${escapeHtml(p)}</p></div>`),
  ].join("");
  wireCopies(el);
}

function renderTunnelEvents(events) {
  const el = $("tunnel-events");
  el.classList.remove("empty");
  if (!events.length) {
    el.classList.add("empty");
    el.textContent = "No tunnel events.";
    return;
  }
  el.innerHTML = events.map((ev) => `<div class="event"><strong>${escapeHtml(ev.type)} ${pill(ev.status)}</strong><span class="meta">${escapeHtml(ev.created_at)} ${escapeHtml(ev.reason_code || "")}</span><p>${escapeHtml(ev.reason || "")}</p></div>`).join("");
}

async function applyTunnelFilter() {
  const params = new URLSearchParams();
  if ($("filter-allocation").value) params.set("allocation_id", $("filter-allocation").value);
  if ($("filter-node").value) params.set("node_id", $("filter-node").value);
  if ($("filter-terminal").checked) params.set("include_terminal", "true");
  const data = await api(`/api/tunnels?${params.toString()}`);
  state.tunnels = data.tunnels || [];
  renderTunnels();
}

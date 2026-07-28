async function refreshAll() {
  try {
    const [summary, health, services, tunnels, quotas, admin, links] = await Promise.all([
      api("/api/summary"),
      api("/api/reconcile-health"),
      api("/api/services"),
      api("/api/tunnels"),
      api("/api/quotas"),
      api("/api/admin"),
      api("/api/links"),
    ]);
    state.services = services.services || [];
    state.reconcileHealth = health || { components: [] };
    state.tunnels = tunnels.tunnels || [];
    state.quotas = quotas.quotas || [];
    state.admin = admin || { retries: [], audit: [] };
    state.summary = summary || null;
    state.links = links;
    renderSummary(summary);
    renderServices();
    renderTunnels();
    renderQuotas();
    renderAdmin();
    renderLinks(links);
    refreshSelectedQuotaEvents();
    setStatus(`Updated ${new Date().toLocaleTimeString()}`);
  } catch (err) {
    setStatus(err.message, true);
  }
}

function resetTimer() {
  if (state.timer) clearInterval(state.timer);
  if ($("auto-refresh").checked) {
    state.timer = setInterval(refreshAll, window.__AXERN_DASHBOARD__.refreshMs || 5000);
  }
}

document.querySelectorAll(".nav").forEach((btn) => btn.addEventListener("click", () => switchView(btn.dataset.view)));
document.querySelectorAll("[data-quota-filter]").forEach((btn) => btn.addEventListener("click", () => {
  state.quotaFilter = btn.dataset.quotaFilter || "all";
  renderQuotas();
}));
$("refresh-now").addEventListener("click", refreshAll);
$("auto-refresh").addEventListener("change", resetTimer);
$("quota-search").addEventListener("input", () => {
  state.quotaSearch = $("quota-search").value;
  renderQuotas();
});
$("open-service").addEventListener("click", () => $("service-search").value && selectService($("service-search").value));
$("open-session").addEventListener("click", () => $("session-search").value && selectTunnel($("session-search").value));
$("apply-tunnel-filter").addEventListener("click", applyTunnelFilter);
wireAdminActions();

refreshAll();
resetTimer();

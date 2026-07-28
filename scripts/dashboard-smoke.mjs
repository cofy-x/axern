#!/usr/bin/env node

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const uiDir = path.join(root, "apps/cli/internal/commands/dashboard/ui");
const styleFiles = ["app.css", "components.css", "quota.css", "admin.css", "responsive.css"];
const scriptFiles = ["common.js", "overview.js", "quota.js", "service.js", "tunnel.js", "admin.js", "links.js", "app.js"];
const html = fs.readFileSync(path.join(uiDir, "index.html"), "utf8");
const dashboardJS = scriptFiles.map((file) => fs.readFileSync(path.join(uiDir, file), "utf8")).join("\n");

assert.match(html, /id="overview-admission-band"/, "overview admission band is addressable");
assert.match(html, /id="overview-quota-pressure-band"/, "overview quota pressure band is addressable");
assert.match(html, /id="overview-reconcile-health-band"/, "overview reconcile health band is addressable");
assert.match(html, /id="overview-reliability-band"/, "overview reliability band is addressable");
assert.match(html, /data-quota-filter="pressure"/, "quota pressure filter exists");
for (const file of scriptFiles) {
  assert.match(html, new RegExp(`src="/${file}"`), `${file} is loaded by index.html`);
}
for (const file of styleFiles) {
  assert.match(html, new RegExp(`href="/${file}"`), `${file} is loaded by index.html`);
}

class ClassList {
  constructor(element) {
    this.element = element;
  }

  add(name) {
    const names = new Set(this.element.className.split(/\s+/).filter(Boolean));
    names.add(name);
    this.element.className = [...names].join(" ");
  }

  remove(name) {
    const names = new Set(this.element.className.split(/\s+/).filter(Boolean));
    names.delete(name);
    this.element.className = [...names].join(" ");
  }

  toggle(name, force) {
    const enabled = force === undefined ? !this.contains(name) : Boolean(force);
    if (enabled) this.add(name);
    else this.remove(name);
    return enabled;
  }

  contains(name) {
    return this.element.className.split(/\s+/).includes(name);
  }
}

class Element {
  constructor(tagName = "div", id = "") {
    this.tagName = tagName.toUpperCase();
    this.id = id;
    this.className = "";
    this.dataset = {};
    this.style = {};
    this.children = [];
    this.listeners = new Map();
    this.hidden = false;
    this.value = "";
    this.checked = false;
    this.disabled = false;
    this.textContent = "";
    this._innerHTML = "";
    this.classList = new ClassList(this);
  }

  get innerHTML() {
    return this._innerHTML;
  }

  set innerHTML(value) {
    this._innerHTML = String(value ?? "");
    this.children = [];
  }

  appendChild(child) {
    this.children.push(child);
    return child;
  }

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) || [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  click() {
    for (const listener of this.listeners.get("click") || []) listener({ target: this });
  }

  querySelectorAll() {
    return [];
  }

  focus() {}
}

class Document {
  constructor() {
    this.elements = new Map();
    this.nav = [];
    this.views = [];
    this.quotaFilters = [];
  }

  createElement(tagName) {
    return new Element(tagName);
  }

  getElementById(id) {
    if (!this.elements.has(id)) this.elements.set(id, new Element("div", id));
    return this.elements.get(id);
  }

  querySelectorAll(selector) {
    switch (selector) {
      case ".nav":
        return this.nav;
      case ".view":
        return this.views;
      case "[data-quota-filter]":
        return this.quotaFilters;
      case "[data-copy]":
        return [];
      default:
        return [];
    }
  }
}

const document = new Document();
for (const id of [
  "status-line",
  "view-title",
  "overview",
  "services",
  "quotas",
  "tunnels",
  "events",
  "links",
  "metric-services",
  "metric-ready",
  "metric-admission",
  "metric-cpu-pressure",
  "metric-memory-pressure",
  "metric-admin-retries",
  "metric-reconcile-failures",
  "metric-tunnels",
  "overview-reconcile-health-band",
  "overview-reconcile-health",
  "overview-admission-band",
  "overview-admission",
  "overview-quota-pressure-band",
  "overview-quota-pressure",
  "overview-services",
  "overview-tunnels",
  "overview-quotas",
  "quota-layout",
  "quota-search",
  "services-list",
  "tunnels-list",
  "quotas-grid",
  "quota-detail",
  "links-list",
  "admin-retry-count",
  "admin-audit-count",
  "admin-retries",
  "admin-audit",
  "admin-action-modal",
  "admin-action-title",
  "admin-action-close",
  "admin-action-impact",
  "admin-action-object",
  "admin-action-reason",
  "admin-action-error",
  "admin-action-submit",
  "service-search",
  "open-service",
  "session-search",
  "open-session",
  "auto-refresh",
  "refresh-now",
  "filter-allocation",
  "filter-node",
  "filter-terminal",
  "apply-tunnel-filter",
  "service-detail",
  "service-events",
  "tunnel-detail",
  "tunnel-events",
]) {
  document.getElementById(id);
}

for (const view of ["overview", "services", "quotas", "tunnels", "admin", "events", "links"]) {
  const viewElement = document.getElementById(view);
  viewElement.className = view === "overview" ? "view active" : "view";
  document.views.push(viewElement);

  const nav = new Element("button");
  nav.className = view === "overview" ? "nav active" : "nav";
  nav.dataset.view = view;
  document.nav.push(nav);
}

for (const filter of ["all", "constrained", "pressure"]) {
  const button = new Element("button");
  button.className = filter === "all" ? "quota-filter active" : "quota-filter";
  button.dataset.quotaFilter = filter;
  button.dataset.quotaLabel = filter[0].toUpperCase() + filter.slice(1);
  document.quotaFilters.push(button);
}

document.getElementById("auto-refresh").checked = false;

const payloads = {
  "/api/summary": {
    services: { total: 2, ready: 1, admission_blocked: 1 },
    quotas: { cpu_pressure: 1, memory_pressure: 0 },
    tunnels: { total: 1 },
    reliability: {
      status: "degraded",
      consistency_status: "inconsistent",
      consistency_issues: 1,
      allocation_lifecycle_retries: 1,
      due_allocation_lifecycle_retries: 1,
      reconcile_unhealthy_components: 1,
      issues: [
        {
          code: "service_reference_ended_allocation",
          severity: "error",
          allocation_id: "alloc-ended",
          owner_type: "service",
          owner_id: "svc-blocked",
          repair_owner: "service_controller",
          repair_action: "service_reconcile",
          repair_target_type: "service",
          repair_target_id: "svc-blocked",
          detail: "service still references an ended allocation",
        },
      ],
      signals: [
        {
          code: "allocation_lifecycle_retries",
          message: "1 allocation lifecycle retry item(s), 1 due",
        },
      ],
    },
  },
  "/api/reconcile-health": {
    components: [
      {
        component: "service",
        last_error: "database unavailable",
        consecutive_failures: 2,
      },
    ],
  },
  "/api/services": {
    services: [
      {
        id: "svc-blocked",
        namespace: "default",
        status: "pending",
        admission_summary: "node CPU capacity exhausted",
        diagnostic_code: "admission_blocked",
        diagnostic_message: "no eligible node: rejection_reasons=insufficient_cpu",
        resources: {
          requests: { cpu_milli: 500, memory_bytes: 512 * 1024 * 1024 },
          limits: { cpu_milli: 1000, memory_bytes: 1024 * 1024 * 1024 },
        },
      },
      { id: "svc-ready", namespace: "default", status: "ready", ready_replicas: 1, replicas: 1 },
    ],
  },
  "/api/tunnels": { tunnels: [{ session_id: "tun-1", status: "ready", allocation_id: "alloc-1" }] },
  "/api/quotas": {
    quotas: [
      {
        namespace: "default",
        version: 2,
        reserved_cpu_milli: 900,
        cpu_milli_limit: 1000,
        available_cpu_milli: 100,
        cpu_usage_percent: 90,
        reserved_memory_bytes: 128 * 1024 * 1024,
        memory_bytes_limit: 1024 * 1024 * 1024,
        available_memory_bytes: 896 * 1024 * 1024,
        memory_usage_percent: 12.5,
      },
      {
        namespace: "dev",
        version: 1,
        reserved_cpu_milli: 100,
        cpu_milli_limit: 1000,
        available_cpu_milli: 900,
        cpu_usage_percent: 10,
        reserved_memory_bytes: 0,
        memory_bytes_limit: null,
        available_memory_bytes: null,
      },
      { namespace: "unlimited", version: 1, reserved_cpu_milli: 0, reserved_memory_bytes: 0 },
    ],
  },
  "/api/admin": {
    retries: [
      {
        allocation_id: "alloc-retry",
        owner_type: "service",
        owner_id: "svc-blocked",
        reason: "create",
        node_id: "node-a",
        reconcile_attempts: 2,
        due: true,
      },
    ],
    audit: [
      {
        event_id: "audit-1",
        operation: "force_allocation_lifecycle_retry",
        target_type: "allocation",
        target_id: "alloc-retry",
        operator_reason: "operator checked node recovery",
        created_at: "2026-05-10T12:00:00Z",
      },
    ],
  },
  "/api/links": { links: [] },
};

const posted = [];
let postCloseDisabled = false;
let postModalHiddenAfterClose = true;
const context = {
  console,
  document,
  navigator: { clipboard: { writeText: () => undefined } },
  window: {
    __AXERN_DASHBOARD__: { refreshMs: 5000 },
    open: () => undefined,
  },
  URLSearchParams,
  setInterval: () => 1,
  clearInterval: () => undefined,
  fetch: async (resource, options = {}) => {
    if (options.method === "POST") {
      posted.push({ resource, body: JSON.parse(options.body || "{}") });
      postCloseDisabled = document.getElementById("admin-action-close").disabled;
      document.getElementById("admin-action-close").click();
      postModalHiddenAfterClose = document.getElementById("admin-action-modal").hidden;
      return {
        ok: true,
        statusText: "OK",
        json: async () => ({
          retry: {
            allocation_id: "alloc-retry",
            owner_type: "service",
            owner_id: "svc-blocked",
            reason: "create",
          },
        }),
      };
    }
    if (!Object.hasOwn(payloads, resource)) throw new Error(`unexpected dashboard API call: ${resource}`);
    return {
      ok: true,
      statusText: "OK",
      json: async () => payloads[resource],
    };
  },
  __dashboardSmoke: {},
};

vm.runInNewContext(`${dashboardJS}
Object.assign(globalThis.__dashboardSmoke, {
  getState: () => state,
  renderServiceDetail,
  renderSummary,
  renderQuotas,
  renderAdmin,
  openAdminAction,
  submitAdminAction,
});
`, context, { filename: "app.js" });

await new Promise((resolve) => setImmediate(resolve));
await new Promise((resolve) => setImmediate(resolve));

assert.equal(document.getElementById("metric-services").textContent, 2);
assert.equal(document.getElementById("metric-cpu-pressure").textContent, 1);
assert.equal(document.getElementById("metric-admin-retries").textContent, 1);
assert.equal(document.getElementById("metric-reliability").textContent, "degraded");
assert.equal(document.getElementById("metric-reconcile-failures").textContent, 1);
assert.equal(document.getElementById("overview-reliability-band").hidden, false);
assert.equal(document.getElementById("overview-reliability").children.length, 2);
assert.match(document.getElementById("overview-reliability").children[0].innerHTML, /service\/svc-blocked/);
assert.equal(document.getElementById("overview-reconcile-health-band").hidden, false);
assert.equal(document.getElementById("overview-reconcile-health").children.length, 1);
assert.equal(document.getElementById("overview-admission-band").hidden, false);
assert.equal(document.getElementById("overview-quota-pressure-band").hidden, false);
assert.equal(document.getElementById("overview-quota-pressure").children.length, 1);
assert.equal(document.getElementById("overview-quotas").children[0].className, "quota-card warning");
assert.equal(document.getElementById("admin-retries").children.length, 1);
assert.match(document.getElementById("admin-audit").innerHTML, /operator checked node recovery/);

context.__dashboardSmoke.openAdminAction("force", payloads["/api/admin"].retries[0]);
document.getElementById("admin-action-reason").value = "operator checked node recovery";
await context.__dashboardSmoke.submitAdminAction();
assert.deepEqual(posted, [{
  resource: "/api/admin/allocation-retries/alloc-retry/force",
  body: { reason: "create", operator_reason: "operator checked node recovery" },
}]);
assert.equal(postCloseDisabled, true);
assert.equal(postModalHiddenAfterClose, false);
assert.equal(document.getElementById("admin-action-modal").hidden, true);

context.__dashboardSmoke.renderServiceDetail({
  service: payloads["/api/services"].services[0],
  replicas: [],
  events: [],
});
assert.match(document.getElementById("service-detail").innerHTML, /Resources/);
assert.match(document.getElementById("service-detail").innerHTML, /diagnostic message/);

const state = context.__dashboardSmoke.getState();
state.quotaFilter = "pressure";
context.__dashboardSmoke.renderQuotas();
assert.equal(document.getElementById("quotas-grid").children.length, 1);
assert.match(document.getElementById("quota-detail").innerHTML, /Admission Signals/);

state.quotaFilter = "constrained";
context.__dashboardSmoke.renderQuotas();
assert.equal(document.getElementById("quotas-grid").children.length, 2);

state.quotaSearch = "missing";
context.__dashboardSmoke.renderQuotas();
assert.equal(document.getElementById("quotas-grid").classList.contains("empty"), true);
assert.equal(document.getElementById("quota-detail").hidden, true);
state.quotaSearch = "";

state.services = [];
state.quotas = [];
state.reconcileHealth = { components: [] };
context.__dashboardSmoke.renderSummary({
  services: { total: 0, ready: 0, admission_blocked: 0 },
  quotas: { cpu_pressure: 0, memory_pressure: 0 },
  tunnels: { total: 0 },
  reliability: { status: "ok", signals: [] },
});
assert.equal(document.getElementById("overview-reliability-band").hidden, true);
assert.equal(document.getElementById("overview-reconcile-health-band").hidden, true);
assert.equal(document.getElementById("overview-admission-band").hidden, true);
assert.equal(document.getElementById("overview-quota-pressure-band").hidden, true);

console.log("dashboard_smoke_ok=true");

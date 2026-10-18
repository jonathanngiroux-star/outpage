// app.test.mjs — automated frontend test for the Outpage desktop GUI.
// Loads the REAL app.js in a vm context with a minimal DOM shim and a
// mock binding layer that mirrors the Go desktopApp contract (the Go
// side is covered by cmd/outpage/desktop_test.go; this file covers the
// frontend's behavior against that contract).
//
// Run: node cmd/outpage/frontend/test/app.test.mjs
// (zero dependencies; plain node assertions)

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import vm from "node:vm";
import assert from "node:assert";

const here = path.dirname(fileURLToPath(import.meta.url));
const appJS = readFileSync(path.join(here, "..", "app.js"), "utf8");

const ETH = "0x85ee7E71f762d772599cbF1EC20E651B30657521";
const BTC = "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg";

// ---------- DOM shim ---------------------------------------------------

class El {
  constructor(id) {
    this.id = id;
    this._html = "";
    this.textContent = "";
    this.value = "";
    this.placeholder = "";
    this.dataset = {};
    this.style = {};
    this.srcdoc = "";
    this._classes = new Set();
    this._listeners = {};
    this.children = [];
  }
  set innerHTML(h) { this._html = String(h); this.children = []; }
  get innerHTML() { return this._html; }
  set className(c) { this._classes = new Set(String(c).split(/\s+/).filter(Boolean)); }
  get className() { return [...this._classes].join(" "); }
  get classList() {
    const el = this;
    return {
      add: (...cs) => cs.forEach(c => el._classes.add(c)),
      remove: (...cs) => cs.forEach(c => el._classes.delete(c)),
      toggle: (c, force) => {
        if (force === undefined) { el._classes.has(c) ? el._classes.delete(c) : el._classes.add(c); }
        else { force ? el._classes.add(c) : el._classes.delete(c); }
      },
      contains: c => el._classes.has(c),
    };
  }
  appendChild(ch) { this.children.push(ch); this._html += ch.outerHTML(); }
  addEventListener(ev, fn) { (this._listeners[ev] ||= []).push(fn); }
  fire(ev, arg) { (this._listeners[ev] || []).forEach(fn => fn(arg)); }
  outerHTML() {
    return `<input id="${this.id}" value="${this.value}" placeholder="${this.placeholder}">`;
  }
}

function makeDocument() {
  const byId = new Map();
  const get = id => { if (!byId.has(id)) byId.set(id, new El(id)); return byId.get(id); };
  const doc = {
    getElementById: get,
    createElement: () => new El("dyn-" + Math.random().toString(36).slice(2)),
    body: new El("body"),
    _keydownHandlers: [],
    addEventListener: (ev, fn) => { if (ev === "keydown") doc._keydownHandlers.push(fn); },
    keydown: arg => doc._keydownHandlers.forEach(fn => fn(arg)),
  };
  const sections = ["components", "incidents", "maintenance", "subscribers", "migrate", "donate", "page"]
    .map(name => { const el = new El("view-" + name); el.dataset.view = name; byId.set(el.id, el); return el; });
  const navButtons = sections.map(s => {
    const b = new El("nav-" + s.dataset.view);
    b.dataset.view = s.dataset.view;
    byId.set(b.id, b);
    return b;
  });
  doc.querySelectorAll = sel => {
    if (sel === "nav button[data-view]") return [...navButtons];
    if (sel === "main > section") return [...sections];
    return [];
  };
  doc._sections = sections;
  doc._navButtons = navButtons;
  return doc;
}

// ---------- mock binding layer (mirrors the Go desktopApp contract) ----

function makeMockBackend(opts = {}) {
  const state = {
    pageName: "",
    components: [],
    incidents: [],
    subscribers: [],
    updates: {},   // id -> [{status, body}]
    wizardDone: false,
    calls: [],
  };
  let incSeq = 0;

  const WIZ_STEPS = [
    { id: "welcome", title: "Welcome to Outpage", body: "\u2026", fields: [], next_label: "Next", optional: false },
    { id: "page", title: "Step 1", body: "\u2026", fields: [{ key: "page_name", label: "Page name", placeholder: "Example Cloud" }], next_label: "Next", optional: false },
    { id: "component", title: "Step 2", body: "\u2026", fields: [{ key: "component_name", label: "Name", placeholder: "API" }, { key: "component_description", label: "Description", placeholder: "" }], next_label: "Next", optional: false },
    { id: "incident", title: "Step 3", body: "\u2026", fields: [{ key: "incident_name", label: "Title", placeholder: "" }, { key: "incident_impact", label: "Impact", placeholder: "minor" }], next_label: "Next", optional: false },
    { id: "subscriber", title: "Step 4", body: "\u2026", fields: [{ key: "subscriber_email", label: "Email", placeholder: "ops@example.net" }], next_label: "Next", optional: true },
    { id: "migrate", title: "Step 5", body: "\u2026", fields: [], next_label: "Next", optional: true, migrate: "Sample: mapped=7 partial=3 unmapped=2" },
    { id: "donate", title: "Done", body: "\u2026Ethereum / USDC (ERC-20): " + ETH + " Bitcoin: " + BTC + "\u2026Finish to open the main view.", fields: [], next_label: "Finish", optional: false },
  ];
  const wiz = { step: 0, values: {}, finished: false };

  const stepView = () => {
    // Clamp to the last step after finishing (the Go binding returns the
    // last step's view with finished=true).
    const s = WIZ_STEPS[Math.min(wiz.step, WIZ_STEPS.length - 1)];
    return {
      step: { id: s.id, title: s.title, body: s.body, fields: s.fields, next_label: s.next_label, optional: s.optional, migrate: s.migrate || "" },
      values: { ...wiz.values },
      finished: wiz.finished,
    };
  };

  const delay = opts.slow ? () => new Promise(() => {}) : () => Promise.resolve();

  const App = {
    async DonateAddresses() { return { ethereum: ETH, bitcoin: BTC, note: "Donations are the only funding \u2014 no feature is gated, ever." }; },
    async Components() { await delay(); return state.components.map(c => ({ ...c })); },
    async CreateComponent(name, description) {
      state.calls.push(["CreateComponent", name, description]);
      if (!name || !name.trim()) return Promise.reject(new Error("component_name: a component name is required"));
      if (state.components.some(c => c.name.toLowerCase() === name.trim().toLowerCase())) {
        return Promise.reject(new Error('duplicate: component "' + name + '"'));
      }
      state.components.push({ id: state.components.length + 1, name: name.trim(), description: description || "", status: "operational" });
    },
    async SetComponentStatus(name, status) { state.calls.push(["SetComponentStatus", name, status]); },
    async OpenIncident(name, impact, components) {
      state.calls.push(["OpenIncident", name, impact, components]);
      const id = ++incSeq;
      state.incidents.push({ id, kind: "incident", name, status: "investigating", impact });
      state.updates[id] = [{ id: 1, status: "investigating", body: "Incident opened." }];
      return { id, kind: "incident", name, status: "investigating", impact };
    },
    async UpdateIncident(id, status, body) {
      state.calls.push(["UpdateIncident", id, status, body]);
      const inc = state.incidents.find(i => i.id === id);
      if (!inc) return Promise.reject(new Error("not found: incident " + id));
      const legal = { investigating: ["identified", "monitoring", "resolved"], identified: ["monitoring", "resolved"], monitoring: ["resolved"], resolved: [] };
      if (!(legal[inc.status] || []).includes(status)) {
        return Promise.reject(new Error('illegal status transition (incident ' + id + ' is currently "' + inc.status + '")'));
      }
      inc.status = status;
      state.updates[id].push({ id: state.updates[id].length + 1, status, body });
    },
    async Incidents() { await delay(); return state.incidents.map(i => ({ ...i })); },
    async IncidentUpdates(id) { return (state.updates[id] || []).map(u => ({ ...u })); },
    async IncidentComponentNames(id) { return []; },
    async AddMaintenance(name, comps, start, end) {
      state.calls.push(["AddMaintenance", name, comps, start, end]);
      if (start >= end) return Promise.reject(new Error("window ends before it starts"));
    },
    async AddSubscriber(kind, address, secret) {
      state.calls.push(["AddSubscriber", kind, address, secret]);
      if (kind === "sms") return Promise.reject(new Error("no SMS, ever"));
      state.subscribers.push({ id: state.subscribers.length + 1, kind, address, secret });
    },
    async Subscribers() {
      await delay();
      return state.subscribers.map(s => ({ id: s.id, kind: s.kind, display: s.kind === "email" ? "o***@e***.net" : "hooks.example.net/***" }));
    },
    async SubscriberCounts() {
      return [state.subscribers.filter(s => s.kind === "email").length,
              state.subscribers.filter(s => s.kind === "webhook").length];
    },
    async RemoveSubscriber(id) { state.subscribers = state.subscribers.filter(s => s.id !== id); },
    async MigrateDryRun(input) {
      state.calls.push(["MigrateDryRun", input]);
      return {
        source: "statuspage", dry_run: true,
        counts: { mapped: 7, partial: 3, unmapped: 2 },
        mappings: [
          { key: "component", source_id: "c1", name: "API", result: "mapped" },
          { key: "subscriber.sms", source_id: "s04", name: "+1555\u2026", result: "unmapped", note: "no SMS gateway, by scope lock" },
        ],
        risks: [{ key: "webhook.secrets", note: "re-enter signing secrets after import" }],
      };
    },
    async PublicPagePreview() { state.calls.push(["PublicPagePreview"]); return "<html>preview with " + (state.components[0]?.name || "nothing") + "</html>"; },
    async WizardView() { return stepView(); },
    async WizardNext(vals) {
      const s = WIZ_STEPS[wiz.step];
      Object.entries(vals || {}).forEach(([k, v]) => { wiz.values[k] = v; });
      if (s.id === "page") { if (!vals?.page_name?.trim()) return Promise.reject(new Error("page_name: a page name is required")); state.pageName = vals.page_name; }
      if (s.id === "component") {
        if (!vals?.component_name?.trim()) return Promise.reject(new Error("component_name: a component name is required"));
        try { await App.CreateComponent(vals.component_name, vals.component_description || ""); }
        catch (e) { return Promise.reject(new Error("component_name: " + e.message)); }
      }
      if (s.id === "incident") {
        if (!vals?.incident_name?.trim()) return Promise.reject(new Error("incident_name: an incident title is required"));
        await App.OpenIncident(vals.incident_name, vals.incident_impact || "minor", state.components.map(c => c.name));
      }
      if (s.id === "subscriber") { if (vals?.subscriber_email?.trim()) await App.AddSubscriber("email", vals.subscriber_email, ""); }
      wiz.step++;
      if (wiz.step >= WIZ_STEPS.length) { wiz.finished = true; state.wizardDone = true; }
      return stepView();
    },
    async WizardSkip() {
      if (!WIZ_STEPS[wiz.step].optional) return Promise.reject(new Error('step "' + WIZ_STEPS[wiz.step].id + '" is required'));
      wiz.step++;
      return stepView();
    },
    async WizardBack() { if (wiz.step > 0) wiz.step--; return stepView(); },
    async WizardReopen() { wiz.step = 0; wiz.finished = false; return stepView(); },
    async WizardDone() { return state.wizardDone; },
  };
  return { App, state };
}

// ---------- harness ------------------------------------------------------

async function bootApp(opts) {
  const doc = makeDocument();
  const { App, state } = makeMockBackend(opts);
  const ctx = vm.createContext({
    document: doc,
    window: { go: { main: { desktopApp: App } } },
    navigator: { clipboard: { writeText: async () => {} } },
    setTimeout: (fn, ms) => setTimeout(fn, Math.min(ms, 1)),
    console,
    Promise,
  });
  vm.runInContext(appJS, ctx);
  await new Promise(r => setTimeout(r, 30)); // let boot() settle
  return { doc, state, ctx, App };
}

// driveWizard clicks through the wizard UI with the given per-step input
// values (the frontend reads the DOM, so we write inputs then click).
async function driveWizard(doc, ctx, inputsByStep) {
  for (const [stepId, vals] of inputsByStep) {
    // fill this step's inputs
    for (const [k, v] of Object.entries(vals || {})) {
      const el = doc.getElementById("wiz-" + k);
      if (el) el.value = v;
    }
    await ctx.wizardNext();
    await new Promise(r => setTimeout(r, 5));
  }
}

let passed = 0, failed = 0;
async function test(name, fn) {
  try {
    await fn();
    passed++;
    console.log("PASS " + name);
  } catch (e) {
    failed++;
    console.log("FAIL " + name + "\n  " + (e && e.message || e));
  }
}

// ---------- tests ---------------------------------------------------------

// 1. Fresh boot: wizard overlay visible at welcome; lists show Loading…
//    until bindings answer.
await test("fresh boot opens the wizard at welcome", async () => {
  const { doc } = await bootApp({ slow: true });
  const overlay = doc.getElementById("wizard-overlay");
  assert.ok(!overlay.classList.contains("hidden"), "overlay must be visible on a fresh store");
  assert.equal(doc.getElementById("wizard-title").textContent, "Welcome to Outpage");
  assert.ok(doc.getElementById("comp-list").innerHTML.includes("Loading"),
    "lists must show a loading state before bindings answer");
});

// 2. Full wizard happy path through the UI: objects created, overlay
//    hides, main view lands on a NON-EMPTY component list.
await test("wizard happy path creates objects and lands on a non-empty main view", async () => {
  const { doc, state, ctx } = await bootApp({});
  await driveWizard(doc, ctx, [
    ["welcome", {}],
    ["page", { page_name: "Example Cloud" }],
    ["component", { component_name: "API", component_description: "Public REST API" }],
    ["incident", { incident_name: "Elevated errors", incident_impact: "minor" }],
    ["subscriber", { subscriber_email: "ops@example.net" }],
    ["migrate", {}],
    ["donate", {}],
  ]);
  assert.ok(doc.getElementById("wizard-overlay").classList.contains("hidden"), "overlay must hide");
  assert.equal(state.pageName, "Example Cloud");
  assert.equal(state.components.length, 1);
  assert.equal(state.components[0].name, "API");
  assert.equal(state.incidents.length, 1);
  assert.equal(state.subscribers.length, 1);
  assert.ok(state.wizardDone, "wizard_done must persist");
  assert.ok(doc.getElementById("comp-list").innerHTML.includes("API"),
    "main view must land non-empty");
  assert.ok(doc.getElementById("view-components").classList.contains("active"),
    "main view must switch to components");
});

// 3. Wizard donate step carries both addresses byte-for-byte.
await test("wizard last step shows both donate addresses", async () => {
  const { doc, ctx } = await bootApp({});
  await driveWizard(doc, ctx, [
    ["welcome", {}],
    ["page", { page_name: "P" }],
    ["component", { component_name: "API" }],
    ["incident", { incident_name: "I", incident_impact: "minor" }],
    ["subscriber", {}],
  ]);
  // now on migrate (optional) — skip to donate
  await ctx.wizardSkip();
  const body = doc.getElementById("wizard-body").textContent;
  assert.ok(body.includes(ETH), "ETH address missing on donate step");
  assert.ok(body.includes(BTC), "BTC address missing on donate step");
  assert.equal(doc.getElementById("wizard-next").textContent, "Finish");
});

// 4. Wizard validation: empty required field shows the error line and
//    does not advance; duplicate component surfaces through the wizard.
await test("wizard errors surface in the error line and do not advance", async () => {
  const { doc, ctx } = await bootApp({});
  await driveWizard(doc, ctx, [
    ["welcome", {}],
    ["page", { page_name: "P" }],
  ]);
  // empty component name
  doc.getElementById("wiz-component_name").value = "";
  await ctx.wizardNext();
  assert.ok(doc.getElementById("wizard-error").textContent.includes("component_name"),
    "error line must name the field");
  assert.equal(doc.getElementById("wizard-title").textContent, "Step 2", "must stay on step 2");
  // valid, then duplicate via main view
  doc.getElementById("wiz-component_name").value = "API";
  await ctx.wizardNext();
  doc.getElementById("comp-name").value = "API";
  await ctx.createComponent();
  const err = doc.getElementById("comp-error").textContent;
  assert.ok(err.includes("duplicate"), "main-view duplicate must surface: " + err);
});

// 5. Binding errors from the main views are SHOWN, not swallowed.
await test("main-view binding errors render in the view's error line", async () => {
  const { doc, ctx } = await bootApp({});
  doc.getElementById("comp-name").value = "API";
  await ctx.createComponent();
  doc.getElementById("comp-name").value = "API";
  await ctx.createComponent();
  assert.ok(doc.getElementById("comp-error").textContent.includes("duplicate"),
    "comp-error must show the duplicate error");
});

// 6. Operator text is escaped in GUI lists.
await test("operator text is HTML-escaped in lists", async () => {
  const { doc, ctx } = await bootApp({});
  doc.getElementById("comp-name").value = "<script>alert(1)</script>";
  await ctx.createComponent();
  const html = doc.getElementById("comp-list").innerHTML;
  assert.ok(html.includes("\u0026lt;script\u0026gt;") || html.includes("<script>"),
    "escaped form must be present: " + html.slice(0, 200));
  assert.ok(!html.includes("<script>alert"), "raw operator text must NOT reach the DOM");
});

// 7. Keyboard path: Enter in the component field submits.
await test("keyboard: Enter submits the component form", async () => {
  const { doc, state, ctx } = await bootApp({});
  // close the wizard first (main-view fields are unreachable while open)
  await driveWizard(doc, ctx, [
    ["welcome", {}],
    ["page", { page_name: "P" }],
    ["component", { component_name: "Seed" }],
    ["incident", { incident_name: "I", incident_impact: "minor" }],
    ["subscriber", {}],
    ["migrate", {}],
    ["donate", {}],
  ]);
  assert.ok(doc.getElementById("wizard-overlay").classList.contains("hidden"));
  doc.getElementById("comp-name").value = "API";
  doc.keydown({ key: "Enter", target: { id: "comp-name" } });
  await new Promise(r => setTimeout(r, 20));
  assert.ok(state.calls.some(c => c[0] === "CreateComponent" && c[1] === "API"),
    "Enter must trigger CreateComponent");
});

// 8. Incident update + resolve from the GUI, with the illegal-transition
//    error naming the current state.
await test("incident update/resolve UI posts updates and shows transition errors", async () => {
  const { doc, ctx, state } = await bootApp({});
  await driveWizard(doc, ctx, [
    ["welcome", {}],
    ["page", { page_name: "P" }],
    ["component", { component_name: "API" }],
    ["incident", { incident_name: "Elevated errors", incident_impact: "minor" }],
    ["subscriber", {}],
    ["migrate", {}],
    ["donate", {}],
  ]);
  // The wizard-created incident is id 1. Set the inline form and post.
  doc.getElementById("inc-update-status-1").value = "identified";
  doc.getElementById("inc-update-body-1").value = "Found the bad deploy.";
  await ctx.postUpdate(1);
  assert.ok(state.calls.some(c => c[0] === "UpdateIncident" && c[2] === "identified"),
    "UpdateIncident must be called");
  // resolve
  await ctx.postResolve(1);
  assert.ok(state.incidents[0].status === "resolved", "incident must resolve");
  // now an illegal move from resolved:
  doc.getElementById("inc-update-status-1").value = "monitoring";
  doc.getElementById("inc-update-body-1").value = "zombie";
  await ctx.postUpdate(1);
  assert.ok(doc.getElementById("inc-error").textContent.includes("resolved"),
    "illegal transition must name the current state");
});

// 9. Donate view: exact addresses.
await test("donate view shows both addresses exactly", async () => {
  const { doc } = await bootApp({});
  assert.equal(doc.getElementById("donate-eth").textContent, ETH);
  assert.equal(doc.getElementById("donate-btc").textContent, BTC);
  assert.ok(doc.getElementById("app-footer").innerHTML.includes(ETH), "footer must carry ETH");
});

// 10. Subscriber list renders masked displays only.
await test("subscriber list shows masked addresses only", async () => {
  const { doc, ctx } = await bootApp({});
  doc.getElementById("sub-kind").value = "email";
  doc.getElementById("sub-address").value = "ops@example.net";
  await ctx.addSubscriber();
  const html = doc.getElementById("sub-list").innerHTML;
  assert.ok(html.includes("o***@e***.net"), "masked display must render");
  assert.ok(!html.includes("ops@example.net"), "raw address must not render");
});

// 11. Migrate viewer renders the report, including unmapped rows.
await test("migrate viewer renders counts and unmapped rows", async () => {
  const { doc, ctx } = await bootApp({});
  await ctx.runMigrate();
  const summary = doc.getElementById("mig-summary").innerHTML;
  assert.ok(summary.includes("mapped=7"), "summary must show mapped=7");
  const details = doc.getElementById("mig-details").innerHTML;
  assert.ok(details.includes("subscriber.sms"), "unmapped row must render");
  assert.ok(details.includes("webhook.secrets"), "risks must render");
});

// 12. Public page preview: refreshed when switching to the view; reflects
//     the current store.
await test("public page preview reflects the current store", async () => {
  const { doc, ctx, state } = await bootApp({});
  doc.getElementById("comp-name").value = "API";
  await ctx.createComponent();
  ctx.showView("page");
  await new Promise(r => setTimeout(r, 20));
  assert.ok(state.calls.some(c => c[0] === "PublicPagePreview"), "preview must render");
  assert.ok(doc.getElementById("page-frame").srcdoc.includes("API"), "preview must contain the component");
});

// 13. Nav switching works from every button.
await test("nav buttons switch views", async () => {
  const { doc } = await bootApp({});
  doc._navButtons.find(b => b.dataset.view === "donate").fire("click");
  assert.ok(doc.getElementById("view-donate").classList.contains("active"));
  doc._navButtons.find(b => b.dataset.view === "components").fire("click");
  assert.ok(doc.getElementById("view-components").classList.contains("active"));
});

// 14. Help reopens the wizard on a done store.
await test("Help reopens the wizard", async () => {
  const { doc, ctx } = await bootApp({});
  await driveWizard(doc, ctx, [
    ["welcome", {}],
    ["page", { page_name: "P" }],
    ["component", { component_name: "API" }],
    ["incident", { incident_name: "I", incident_impact: "minor" }],
    ["subscriber", {}],
    ["migrate", {}],
    ["donate", {}],
  ]);
  assert.ok(doc.getElementById("wizard-overlay").classList.contains("hidden"));
  await ctx.reopenWizard();
  assert.ok(!doc.getElementById("wizard-overlay").classList.contains("hidden"), "reopen must show overlay");
  assert.equal(doc.getElementById("wizard-title").textContent, "Welcome to Outpage");
});

// 15. The esc() regression guard: operator text with entities and quotes
//     renders escaped everywhere it is interpolated.
await test("esc() survives entity-decoding regressions", async () => {
  const { doc, ctx } = await bootApp({});
  doc.getElementById("comp-name").value = "<img src=x onerror=alert(1)>";
  doc.getElementById("comp-desc").value = "a & b < c";
  await ctx.createComponent();
  const html = doc.getElementById("comp-list").innerHTML;
  assert.ok(!html.includes("<img src=x"), "raw img tag must not reach the DOM");
  assert.ok(!html.includes("a & b < c"), "raw & and < must not reach the DOM");
});

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed ? 1 : 0);

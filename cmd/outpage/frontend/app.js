// app.js: the Outpage desktop frontend. Vanilla JS, no dependencies.
// Every action calls a Go binding (window.go.main.desktopApp.*); errors
// from bindings are shown in the view's error line — never swallowed.
// esc() uses \u0026 escapes so the source never contains a decodable
// entity (a past regression left it a no-op and operator text reached
// the DOM raw).

let App = null; // window.go.main.desktopApp once the runtime injects it

function setErr(id, msg) {
  const el = document.getElementById(id);
  el.textContent = msg || "";
}

function pill(status) {
  return `<span class="status-pill ${status}">${esc(status)}</span>`;
}

// esc escapes operator-supplied text for HTML interpolation. The
// replacement strings are written with \u0026 so no HTML entity ever
// appears in this source file (build/tooling decoded entities once and
// silently disabled escaping — that class of regression is what the
// frontend test guards).
function esc(s) {
  return String(s)
    .replace(/&/g, "\u0026amp;")
    .replace(/</g, "\u0026lt;")
    .replace(/>/g, "\u0026gt;");
}

const LOADING = `<p class="empty">Loading\u2026</p>`;

// ---- boot -----------------------------------------------------------

async function boot() {
  // The Wails runtime injects window.go after page load on slow starts;
  // poll for it before calling anything (the step-4 audit's lesson).
  for (let i = 0; i < 100; i++) {
    if (window.go && window.go.main && window.go.main.desktopApp) {
      App = window.go.main.desktopApp;
      break;
    }
    await new Promise(r => setTimeout(r, 50));
  }
  if (!App) {
    document.body.insertAdjacentHTML("afterbegin",
      `<div class="error">Desktop runtime did not initialize. Restart the app.</div>`);
    return;
  }
  // Nav
  document.querySelectorAll("nav button[data-view]").forEach(btn => {
    btn.addEventListener("click", () => showView(btn.dataset.view));
  });
  // Footer: donate addresses, always.
  const d = await App.DonateAddresses();
  document.getElementById("app-footer").innerHTML =
    `Outpage is free, MIT-licensed software. Donations are the only funding \u2014 never a paywall, never counted as traction.<br>` +
    `Ethereum / USDC (ERC-20): <code>${esc(d.ethereum)}</code> \u00b7 Bitcoin: <code>${esc(d.bitcoin)}</code>`;
  document.getElementById("donate-eth").textContent = d.ethereum;
  document.getElementById("donate-btc").textContent = d.bitcoin;
  document.getElementById("donate-note").textContent = d.note;

  // Loading states before the first answers arrive.
  document.getElementById("comp-list").innerHTML = LOADING;
  document.getElementById("inc-list").innerHTML = LOADING;
  document.getElementById("sub-list").innerHTML = LOADING;

  // The first-run wizard must show immediately — it is never gated
  // behind data loads (a slow store must not blank the front door).
  // List refreshes continue in the background.
  refreshAll().catch(() => {});
  await maybeShowWizard();
}

function showView(name) {
  document.querySelectorAll("main > section").forEach(s => s.classList.remove("active"));
  document.getElementById("view-" + name).classList.add("active");
  document.querySelectorAll("nav button[data-view]").forEach(b =>
    b.classList.toggle("active", b.dataset.view === name));
  if (name === "page") renderPage();
}

async function refreshAll() {
  await Promise.all([refreshComponents(), refreshIncidents(), refreshSubscribers()]);
}

// ---- components -----------------------------------------------------

async function refreshComponents() {
  const el = document.getElementById("comp-list");
  el.innerHTML = LOADING;
  const comps = await App.Components();
  if (!comps || comps.length === 0) {
    el.innerHTML = `<p class="empty">No components yet.</p>`;
    return;
  }
  el.innerHTML = `<table><tr><th>Name</th><th>Status</th><th>Description</th></tr>` +
    comps.map(c =>
      `<tr><td>${esc(c.name)}</td><td>${pill(c.status)}</td><td class="muted">${esc(c.description || "\u2014")}</td></tr>`
    ).join("") + `</table>`;
}

async function createComponent() {
  setErr("comp-error", "");
  try {
    await App.CreateComponent(document.getElementById("comp-name").value,
      document.getElementById("comp-desc").value);
    document.getElementById("comp-name").value = "";
    document.getElementById("comp-desc").value = "";
    await refreshComponents();
  } catch (e) {
    setErr("comp-error", String(e));
  }
}

// ---- incidents ------------------------------------------------------

const UPDATE_STATUSES = ["investigating", "identified", "monitoring", "resolved"];

async function refreshIncidents() {
  const el = document.getElementById("inc-list");
  el.innerHTML = LOADING;
  const incs = await App.Incidents();
  if (!incs || incs.length === 0) {
    el.innerHTML = `<p class="empty">No incidents yet.</p>`;
    return;
  }
  const rows = await Promise.all(incs.map(async inc => {
    const ups = await App.IncidentUpdates(inc.id);
    const comps = await App.IncidentComponentNames(inc.id);
    const statusOptions = UPDATE_STATUSES.map(s =>
      `<option${s === "monitoring" ? " selected" : ""}>${s}</option>`).join("");
    return `<tr><td>${inc.id}</td><td class="muted">${esc(inc.kind)}</td><td>${esc(inc.name)}</td>` +
      `<td>${pill(inc.status)}</td><td class="muted">${esc(inc.impact)}</td>` +
      `<td><button class="ghost" onclick="toggleTimeline(${inc.id})">timeline (${ups.length})</button></td></tr>` +
      `<tr class="timeline-row" id="timeline-${inc.id}" style="display:none">` +
      `<td colspan="6">` +
      (comps && comps.length ? `<p class="muted">Affected: ${esc(comps.join(", "))}</p>` : "") +
      `<ol>${ups.map(u =>
        `<li><b>${esc(u.status)}</b> \u2014 ${esc(u.body)}</li>`).join("")}</ol>` +
      `<div class="row">` +
      `<label for="inc-update-status-${inc.id}">Update status</label>` +
      `<select id="inc-update-status-${inc.id}">${statusOptions}</select>` +
      `<input id="inc-update-body-${inc.id}" placeholder="What changed?">` +
      `<button class="primary" onclick="postUpdate(${inc.id})">Post update</button>` +
      `<button class="ghost" onclick="postResolve(${inc.id})">Resolve</button>` +
      `</div>` +
      `</td></tr>`;
  }));
  el.innerHTML = `<table><tr><th>#</th><th>Kind</th><th>Title</th><th>Status</th><th>Impact</th><th></th></tr>` +
    rows.join("") + `</table>`;
}

function toggleTimeline(id) {
  const row = document.getElementById("timeline-" + id);
  row.style.display = row.style.display === "none" ? "" : "none";
}

async function openIncident() {
  setErr("inc-error", "");
  try {
    const comps = document.getElementById("inc-components").value
      .split(",").map(s => s.trim()).filter(s => s !== "");
    await App.OpenIncident(document.getElementById("inc-name").value,
      document.getElementById("inc-impact").value, comps);
    document.getElementById("inc-name").value = "";
    await refreshIncidents();
  } catch (e) {
    setErr("inc-error", String(e));
  }
}

// postUpdate posts a timeline update from the incident's inline form.
async function postUpdate(id) {
  setErr("inc-error", "");
  const status = document.getElementById("inc-update-status-" + id).value;
  const body = document.getElementById("inc-update-body-" + id).value;
  try {
    await App.UpdateIncident(id, status, body);
    document.getElementById("inc-update-body-" + id).value = "";
    await refreshIncidents();
    const row = document.getElementById("timeline-" + id);
    if (row) row.style.display = "";
    await renderPage();
  } catch (e) {
    setErr("inc-error", String(e));
  }
}

// postResolve resolves the incident with the body text if given, else a
// plain "Resolved."
async function postResolve(id) {
  setErr("inc-error", "");
  const body = document.getElementById("inc-update-body-" + id).value || "Resolved.";
  try {
    await App.UpdateIncident(id, "resolved", body);
    document.getElementById("inc-update-body-" + id).value = "";
    await refreshIncidents();
    await renderPage();
  } catch (e) {
    setErr("inc-error", String(e));
  }
}

// ---- maintenance ------------------------------------------------------

async function addMaintenance() {
  setErr("maint-error", "");
  try {
    const comps = document.getElementById("maint-components").value
      .split(",").map(s => s.trim()).filter(s => s !== "");
    await App.AddMaintenance(document.getElementById("maint-name").value, comps,
      document.getElementById("maint-start").value, document.getElementById("maint-end").value);
    document.getElementById("maint-name").value = "";
    const el = document.getElementById("maint-error");
    el.className = "ok-note";
    el.textContent = "Maintenance scheduled.";
  } catch (e) {
    const el = document.getElementById("maint-error");
    el.className = "error";
    setErr("maint-error", String(e));
  }
}

// ---- subscribers ------------------------------------------------------

async function refreshSubscribers() {
  const el = document.getElementById("sub-list");
  el.innerHTML = LOADING;
  const subs = await App.Subscribers();
  const e = document.getElementById("sub-count");
  const [emailCount, hookCount] = await App.SubscriberCounts();
  e.textContent = `email=${emailCount} webhook=${hookCount}`;
  if (!subs || subs.length === 0) {
    el.innerHTML = `<p class="empty">No subscribers yet.</p>`;
    return;
  }
  el.innerHTML = `<table><tr><th>#</th><th>Kind</th><th>Address (masked)</th><th></th></tr>` +
    subs.map(s => `<tr><td>${s.id}</td><td class="muted">${esc(s.kind)}</td><td class="mono">${esc(s.display)}</td>` +
      `<td><button class="danger" onclick="removeSubscriber(${s.id})">remove</button></td></tr>`).join("") +
    `</table>`;
}

async function addSubscriber() {
  setErr("sub-error", "");
  try {
    await App.AddSubscriber(document.getElementById("sub-kind").value,
      document.getElementById("sub-address").value,
      document.getElementById("sub-secret").value);
    document.getElementById("sub-address").value = "";
    document.getElementById("sub-secret").value = "";
    await refreshSubscribers();
  } catch (e) {
    setErr("sub-error", String(e));
  }
}

async function removeSubscriber(id) {
  try {
    await App.RemoveSubscriber(id);
    await refreshSubscribers();
  } catch (e) {
    setErr("sub-error", String(e));
  }
}

// ---- migrate ----------------------------------------------------------

async function runMigrate() {
  setErr("mig-error", "");
  try {
    const rep = await App.MigrateDryRun(document.getElementById("mig-input").value);
    document.getElementById("mig-summary").innerHTML =
      `<p><b>${esc(rep.source)}</b> dry-run: mapped=${rep.counts.mapped} ` +
      `partial=${rep.counts.partial} unmapped=${rep.counts.unmapped}</p>`;
    const details = (rep.mappings || []).map(m =>
      `<tr><td class="mono">${esc(m.key)}</td><td>${esc(m.source_id)}</td><td>${esc(m.name)}</td>` +
      `<td><b>${esc(m.result)}</b></td><td class="muted">${esc((m.dropped || []).join(", "))}</td>` +
      `<td class="muted">${esc(m.note || "")}</td></tr>`).join("");
    const risks = (rep.risks || []).map(r =>
      `<li><span class="mono">${esc(r.key)}</span> \u2014 ${esc(r.note)}</li>`).join("");
    document.getElementById("mig-details").innerHTML =
      `<table><tr><th>Key</th><th>Source ID</th><th>Name</th><th>Result</th><th>Dropped</th><th>Note</th></tr>${details}</table>` +
      (risks ? `<p><b>Risks:</b></p><ul>${risks}</ul>` : `<p class="muted">No risks reported.</p>`);
  } catch (e) {
    setErr("mig-error", String(e));
  }
}

// ---- donate / page ------------------------------------------------------

function copyText(text, btn) {
  navigator.clipboard.writeText(text).then(() => {
    btn.textContent = "Copied";
    setTimeout(() => { btn.textContent = "Copy"; }, 1200);
  });
}

async function renderPage() {
  const frame = document.getElementById("page-frame");
  try {
    const html = await App.PublicPagePreview();
    frame.srcdoc = html;
  } catch (e) {
    frame.srcdoc = "<p class='error'>" + esc(String(e)) + "</p>";
  }
}

// ---- wizard ------------------------------------------------------------

let wizardState = null;

async function maybeShowWizard() {
  // The wizard runs on first launch (store flag) and can be reopened
  // from Help at any time.
  const st = await App.WizardView();
  renderWizard(st);
  const done = await App.WizardDone();
  document.getElementById("wizard-overlay").classList.toggle("hidden", done);
}

async function reopenWizard() {
  const st = await App.WizardReopen();
  renderWizard(st);
  document.getElementById("wizard-overlay").classList.remove("hidden");
}

function renderWizard(st) {
  wizardState = st;
  document.getElementById("wizard-progress").textContent =
    `Setup wizard \u2014 ${stepIndex(st)} of 7 \u2014 Help reopens this any time`;
  document.getElementById("wizard-title").textContent = st.step.title;
  document.getElementById("wizard-body").textContent = st.step.body;
  // fields
  const fields = document.getElementById("wizard-fields");
  fields.innerHTML = "";
  (st.step.fields || []).forEach(f => {
    const row = document.createElement("div");
    row.className = "row";
    const label = document.createElement("label");
    label.textContent = f.label;
    const input = document.createElement("input");
    input.id = "wiz-" + f.key;
    input.placeholder = f.placeholder || "";
    input.value = st.values && st.values[f.key] ? st.values[f.key] : "";
    row.appendChild(label);
    row.appendChild(input);
    fields.appendChild(row);
  });
  if (st.step.id === "migrate" && st.step.migrate) {
    const p = document.createElement("p");
    p.className = "muted";
    p.textContent = st.step.migrate;
    fields.appendChild(p);
  }
  document.getElementById("wizard-back").style.display =
    st.step.id === "welcome" ? "none" : "";
  document.getElementById("wizard-skip").style.display =
    st.step.optional ? "" : "none";
  document.getElementById("wizard-next").textContent = st.step.next_label || "Next";
  setErr("wizard-error", "");
}

function stepIndex(st) {
  const ids = ["welcome", "page", "component", "incident", "subscriber", "migrate", "donate"];
  const i = ids.indexOf(st.step.id);
  return (i < 0 ? 1 : i + 1) + "/7";
}

async function wizardNext() {
  const vals = {};
  (wizardState.step.fields || []).forEach(f => {
    const el = document.getElementById("wiz-" + f.key);
    if (el) vals[f.key] = el.value;
  });
  try {
    const st = await App.WizardNext(vals);
    renderWizard(st);
    if (st.finished) {
      document.getElementById("wizard-overlay").classList.add("hidden");
      await refreshAll();
      showView("components");
    }
  } catch (e) {
    setErr("wizard-error", String(e));
  }
}

async function wizardSkip() {
  try {
    const st = await App.WizardSkip();
    renderWizard(st);
  } catch (e) {
    setErr("wizard-error", String(e));
  }
}

async function wizardBack() {
  try {
    const st = await App.WizardBack();
    renderWizard(st);
  } catch (e) {
    setErr("wizard-error", String(e));
  }
}

// Keyboard path for primary actions: Enter in a wizard input = Next;
// Enter in a main-view input submits that view's form. The wizard
// overlay owns the keyboard while it is open.
document.addEventListener("keydown", e => {
  if (e.key !== "Enter") return;
  const id = e.target && e.target.id ? e.target.id : "";
  if (id.startsWith("wiz-")) {
    wizardNext();
    return;
  }
  if (!document.getElementById("wizard-overlay").classList.contains("hidden")) {
    return; // wizard open: main-view fields are not reachable
  }
  const submits = {
    "comp-name": createComponent,
    "comp-desc": createComponent,
    "inc-name": openIncident,
    "inc-components": openIncident,
    "maint-name": addMaintenance,
    "maint-start": addMaintenance,
    "maint-end": addMaintenance,
    "sub-address": addSubscriber,
    "sub-secret": addSubscriber,
    "inc-update-body-1": null, // dynamic ids handled below
  };
  delete submits["inc-update-body-1"];
  if (id.startsWith("inc-update-body-")) {
    postUpdate(id.slice("inc-update-body-".length));
    return;
  }
  if (submits[id]) submits[id]();
});

boot();

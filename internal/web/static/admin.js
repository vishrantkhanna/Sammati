"use strict";
// Sammati admin screens. Plain JavaScript, no build step.
const app = document.getElementById("app");
const state = { view: "overview", me: null, note: "", logoVersion: Date.now() };
const brandMark = () => h("p", { class: "brand-block" }, h("img", { src: "/static/logo.svg", alt: "Sammati", width: 150, height: 34 }));

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v == null) continue;
    if (k === "class") el.className = v;
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "value") el.value = v;
    else if (k === "checked" || k === "disabled" || k === "required") el[k] = !!v;
    else el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) {
    if (kid == null || kid === false) continue;
    el.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
  }
  return el;
}

async function api(method, path, body) {
  const res = await fetch("/api/v1" + path, {
    method, credentials: "same-origin",
    headers: { "Content-Type": "application/json", "X-Sammati-Admin": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let data = {};
  try { data = await res.json(); } catch (e) { /* empty body */ }
  // A session that ended while the screens were open: back to sign-in.
  if (res.status === 401 && path.startsWith("/admin") && state.me) { state.me = null; start(); }
  if (!res.ok) { const err = new Error(data.detail || "Something went wrong."); err.data = data; err.status = res.status; throw err; }
  return data;
}

const when = (iso) => new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
const field = (label, input, hint) => h("div", {}, h("label", {}, label, input), hint ? h("p", { class: "hint" }, hint) : null);
const input = (attrs) => h("input", Object.assign({ type: "text" }, attrs));
function message(text, bad) { return text ? h("p", { class: "msg" + (bad ? " bad" : ""), role: bad ? "alert" : "status" }, text) : null; }

// form() builds a form whose submit handler may throw; the error is shown.
function form(onsubmit, ...kids) {
  const err = h("div", {});
  const f = h("form", { onsubmit: async (e) => {
    e.preventDefault(); err.replaceChildren();
    const btn = f.querySelector("button[type=submit]"); if (btn) btn.disabled = true;
    try { await onsubmit(new FormData(f), f); } catch (ex) { err.replaceChildren(message(ex.message, true)); }
    if (btn) btn.disabled = false;
  } }, ...kids, err);
  return f;
}

async function start() {
  try {
    const setup = await api("GET", "/setup");
    if (!setup.setup_done) return app.replaceChildren(setupScreen(setup.responsibility));
    try { state.me = await api("GET", "/admin/me"); } catch (e) { return app.replaceChildren(loginScreen()); }
    render();
  } catch (e) { app.replaceChildren(h("p", { class: "narrow msg bad" }, "Sammati could not be reached. Is the server running?")); }
}

function setupScreen(responsibility) {
  return h("div", { class: "narrow" },
    brandMark(),
    h("h1", {}, "Welcome to Sammati"),
    h("p", { class: "muted" }, "Set up the administrator for this installation. This screen appears only once."),
    h("div", { class: "card" }, form(async (d) => {
      await api("POST", "/setup", { organisation: d.get("organisation"), email: d.get("email"), password: d.get("password"), accept: d.get("accept") === "on" });
      start();
    },
      field("Organisation Name", input({ name: "organisation", required: true, maxlength: 120 }), "Shown on your consent forms."),
      field("Your Email", input({ name: "email", type: "email", required: true, autocomplete: "username" })),
      field("Password", input({ name: "password", type: "password", required: true, minlength: 12, autocomplete: "new-password" }), "At least 12 characters."),
      h("h3", {}, "Your Responsibility"),
      h("p", { class: "note" }, responsibility),
      h("label", { class: "check" }, h("input", { type: "checkbox", name: "accept", required: true }),
        h("span", {}, "I understand that I run this installation and am responsible for it.")),
      h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Finish Setup")))));
}

function loginScreen() {
  const code = h("div", { class: "hide" }, field("Code From Your Authenticator App", input({ name: "code", inputmode: "numeric", autocomplete: "one-time-code", maxlength: 7 })));
  return h("div", { class: "narrow" },
    brandMark(),
    h("div", { class: "card" }, h("h2", {}, "Sign In"), form(async (d) => {
      try { await api("POST", "/login", { email: d.get("email"), password: d.get("password"), code: d.get("code") || "" }); }
      catch (e) { if (e.data && e.data.code_needed) { code.classList.remove("hide"); code.querySelector("input").focus(); } throw e; }
      start();
    },
      field("Email", input({ name: "email", type: "email", required: true, autocomplete: "username" })),
      field("Password", input({ name: "password", type: "password", required: true, autocomplete: "current-password" })),
      code,
      h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Sign In")))));
}

const views = {
  overview: ["Overview", overview], purposes: ["Purposes", purposes], notices: ["Notices", notices], forms: ["Forms", forms],
  records: ["Records", records], integrations: ["Integrations", integrations], settings: ["Settings", settings],
};

async function render() {
  const body = h("div", {}, h("p", { class: "muted" }, "Loading…"));
  const nav = Object.entries(views).map(([key, [label]]) =>
    h("button", { type: "button", class: key === state.view ? "on" : "", "aria-current": key === state.view ? "page" : null, onclick: () => { state.view = key; state.note = ""; render(); } }, label));
  app.replaceChildren(
    h("header", { class: "top" }, h("div", { class: "wrap" },
      // With a company logo, the company leads and Sammati steps back to a
      // small "Powered by" line beside it.
      state.me.has_logo
        ? h("a", { class: "brand company", href: "/admin/" }, h("img", { src: "/logo?v=" + state.logoVersion, alt: state.me.admin.organisation }),
            h("span", { class: "powered-by" }, "Powered by Sammati"))
        : h("a", { class: "brand", href: "/admin/" }, h("img", { src: "/static/mark.svg", alt: "" }), "Sammati", h("span", { class: "org" }, state.me.admin.organisation)),
      h("nav", { class: "nav", "aria-label": "Sections" }, nav),
      h("button", { class: "btn plain small", type: "button", onclick: async () => { await api("POST", "/logout"); state.me = null; start(); } }, "Sign Out"))),
    h("main", { class: "wrap" }, state.note ? message(state.note) : null, body),
    h("footer", { class: "wrap foot" }, h("img", { class: "foot-logo", src: "/static/logo.svg", alt: "Sammati", width: 110, height: 25 }),
      h("p", {}, state.me.responsibility), h("p", {}, "Sammati " + state.me.version + " · MIT licence")));
  try { body.replaceChildren(...[await views[state.view][1]()].flat().filter(Boolean)); }
  catch (e) { if (state.me) body.replaceChildren(message(e.message, true)); }
}
const done = (note) => { state.note = note; render(); };

async function overview() {
  const s = await api("GET", "/admin/stats");
  const stat = (n, label) => h("div", { class: "stat" }, h("b", {}, n), h("span", {}, label));
  const step = (ok, text, view) => h("li", {}, ok ? "Done: " : "", h("a", { href: "#", onclick: (e) => { e.preventDefault(); state.view = view; render(); } }, text));
  return [h("h1", {}, "Overview"),
    h("div", { class: "grid" }, stat(s.people, "People With a Record"), stat(s.grants_30_days, "Consents, Last 30 Days"),
      stat(s.withdrawals_30_days, "Withdrawals, Last 30 Days"), stat(s.records, "Records in Total")),
    h("div", { class: "card" }, h("h2", {}, "Getting Started"),
      h("ol", {}, step(s.purposes > 0, "Add a purpose: one reason you use personal data", "purposes"),
        step(false, "Publish a notice: what you tell people before they agree", "notices"),
        step(s.forms > 0, "Create a form and put it on your website", "forms"),
        step(false, "Add a webhook or API key to connect your own systems", "integrations"))),
    s.webhooks_failed ? message(s.webhooks_failed + " webhook deliveries failed. See Integrations.", true) : null];
}

async function purposes() {
  const list = (await api("GET", "/admin/purposes")).data;
  return [h("h1", {}, "Purposes"), h("p", { class: "muted" }, "A purpose is one specific reason you use personal data. People agree to each purpose separately."),
    h("div", { class: "card scroll" }, list.length ? h("table", {}, h("thead", {}, h("tr", {}, h("th", {}, "Code"), h("th", {}, "Title"), h("th", {}, "Description"), h("th", {}, ""))),
      h("tbody", {}, list.map((p) => h("tr", {}, h("td", { class: "mono" }, p.code), h("td", {}, p.title), h("td", {}, p.description),
        h("td", {}, p.archived ? h("span", { class: "pill off" }, "Archived") : h("button", { class: "btn danger small", type: "button", onclick: async () => {
          if (confirm("Archive " + p.title + "? It stays on existing forms and records but cannot be added to new forms.")) { await api("POST", "/admin/purposes/" + p.code + "/archive"); done("Purpose archived."); } } }, "Archive")))))) : h("p", { class: "muted" }, "No purposes yet.")),
    h("div", { class: "card" }, h("h2", {}, "Add a Purpose"), form(async (d) => {
      await api("POST", "/admin/purposes", { code: d.get("code"), title: d.get("title"), description: d.get("description") }); done("Purpose added.");
    },
      field("Code", input({ name: "code", required: true, pattern: "[a-z][a-z0-9_\\-]*", maxlength: 64 }), "Lowercase letters, digits, _ or -, for example marketing_email. It cannot be changed later."),
      field("Title", input({ name: "title", required: true, maxlength: 120 }), "Shown to people next to the tick box."),
      field("Description", h("textarea", { name: "description", required: true, maxlength: 1000 }), "Say simply what you will do with the data."),
      h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Add Purpose"))))];
}

async function notices() {
  const all = (await api("GET", "/admin/notices")).data;
  const latest = {};
  for (const n of all) { const cur = latest[n.slug]; if (!cur || n.version > cur.version) latest[n.slug] = { version: n.version, at: n.created_at, langs: [n.lang], title: n.title }; else if (n.version === cur.version) { cur.langs.push(n.lang); if (n.lang === "en") cur.title = n.title; } }
  const slugs = Object.keys(latest);
  return [h("h1", {}, "Notices"), h("p", { class: "muted" }, "The notice is what a person reads before agreeing. Each published version is kept for good and stored with every consent given under it."),
    h("div", { class: "card scroll" }, slugs.length ? h("table", {}, h("thead", {}, h("tr", {}, h("th", {}, "Notice"), h("th", {}, "Title"), h("th", {}, "Current Version"), h("th", {}, "Languages"), h("th", {}, "Published"))),
      h("tbody", {}, slugs.map((s) => h("tr", {}, h("td", { class: "mono" }, s), h("td", {}, latest[s].title), h("td", {}, latest[s].version), h("td", {}, latest[s].langs.join(", ")), h("td", {}, when(latest[s].at)))))) : h("p", { class: "muted" }, "No notices yet.")),
    h("div", { class: "card" }, h("h2", {}, "Publish a Notice"), h("p", { class: "muted" }, "Use an existing name to publish its next version, or a new name to start a new notice."),
      form(async (d) => {
        const texts = [{ lang: "en", title: d.get("title_en"), body: d.get("body_en") }];
        if (d.get("body_hi").trim()) texts.push({ lang: "hi", title: d.get("title_hi") || d.get("title_en"), body: d.get("body_hi") });
        if (d.get("lang_x").trim() && d.get("body_x").trim()) texts.push({ lang: d.get("lang_x").trim(), title: d.get("title_x") || d.get("title_en"), body: d.get("body_x") });
        const r = await api("POST", "/admin/notices", { slug: d.get("slug"), texts }); done("Published version " + r.version + " of " + r.slug + ".");
      },
        field("Notice Name", input({ name: "slug", required: true, pattern: "[a-z][a-z0-9_\\-]*", maxlength: 64, list: "slugs" }), "Lowercase, for example signup."),
        h("datalist", { id: "slugs" }, slugs.map((s) => h("option", { value: s }))),
        h("h3", {}, "English"), field("Title", input({ name: "title_en", required: true, maxlength: 200 })),
        field("Notice Text", h("textarea", { name: "body_en", required: true }), "What data you collect, why, how long you keep it, and how to withdraw or complain."),
        h("h3", {}, "Hindi (Optional)"), field("Title", input({ name: "title_hi", maxlength: 200 })), field("Notice Text", h("textarea", { name: "body_hi" })),
        h("h3", {}, "Another Language (Optional)"), field("Language Code", input({ name: "lang_x", maxlength: 10 }), "For example ta, bn or mr."),
        field("Title", input({ name: "title_x", maxlength: 200 })), field("Notice Text", h("textarea", { name: "body_x" })),
        h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Publish"))))];
}

async function forms() {
  const [list, purposeList, noticeList] = await Promise.all([api("GET", "/admin/forms"), api("GET", "/admin/purposes"), api("GET", "/admin/notices")]);
  const active = purposeList.data.filter((p) => !p.archived);
  const slugs = [...new Set(noticeList.data.map((n) => n.slug))];
  const origin = location.origin;
  const card = (f) => {
    const snippet = '<script src="' + origin + '/widget.js" data-form="' + f.id + '"></' + 'script>';
    return h("div", { class: "card" }, h("div", { class: "between" }, h("h2", {}, f.title, " ", h("span", { class: "pill" + (f.active ? "" : " off") }, f.active ? "On" : "Off")),
      h("button", { class: "btn plain small", type: "button", onclick: async () => { await api("POST", "/admin/forms/" + f.id + "/active", { active: !f.active }); done(f.active ? "Form switched off." : "Form switched on."); } }, f.active ? "Switch Off" : "Switch On")),
      h("p", { class: "muted" }, "Notice: ", h("code", {}, f.notice_slug), " · Purposes: ", f.purposes.map((p) => p.title + (p.required ? " (required)" : "")).join(", ")),
      h("p", {}, "Link: ", h("a", { href: "/f/" + f.id, target: "_blank", rel: "noopener" }, origin + "/f/" + f.id)),
      h("details", {}, h("summary", {}, "Put It on Your Website"), h("p", { class: "hint" }, "Paste this where the form should appear:"), h("pre", {}, snippet)));
  };
  return [h("h1", {}, "Forms"), h("p", { class: "muted" }, "A form shows one notice and lets a person agree to purposes. Share its link or embed it on your site."),
    ...list.data.map(card), list.data.length ? null : h("div", { class: "card muted" }, "No forms yet."),
    h("div", { class: "card" }, h("h2", {}, "Create a Form"),
      !active.length || !slugs.length ? h("p", { class: "msg bad" }, "Add at least one purpose and publish one notice first.") :
      form(async (d) => {
        const chosen = active.filter((p) => d.get("p_" + p.code) === "on").map((p) => ({ purpose: p.code, required: d.get("r_" + p.code) === "on" }));
        await api("POST", "/admin/forms", { id: d.get("id"), title: d.get("title"), notice_slug: d.get("notice"), identifier_label: d.get("label"), success_message: d.get("success"), purposes: chosen });
        done("Form created.");
      },
        field("Form Name", input({ name: "id", required: true, pattern: "[a-z][a-z0-9_\\-]*", maxlength: 64 }), "Lowercase, used in the link, for example newsletter."),
        field("Title", input({ name: "title", required: true, maxlength: 120 })),
        field("Notice", h("select", { name: "notice" }, slugs.map((s) => h("option", { value: s }, s)))),
        h("label", {}, "Purposes"), h("p", { class: "hint" }, "Mark a purpose Required only when your service cannot work without it."),
        active.map((p) => h("div", { class: "between" }, h("label", { class: "check" }, h("input", { type: "checkbox", name: "p_" + p.code }), h("span", {}, h("b", {}, p.title), p.description)),
          h("label", { class: "check" }, h("input", { type: "checkbox", name: "r_" + p.code }), h("span", {}, "Required")))),
        field("What You Ask People For", input({ name: "label", value: "Email address", maxlength: 60 }), "The identifier that links a person to their consent."),
        field("Message After Submitting", input({ name: "success", value: "Thank you. Your choices have been saved.", maxlength: 300 })),
        h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Create Form"))))];
}

async function records() {
  const out = h("div", {});
  const person = h("div", {});
  const show = (list) => out.replaceChildren(list.length ? h("div", { class: "scroll" }, h("table", {}, h("thead", {}, h("tr", {}, h("th", {}, "No."), h("th", {}, "When"), h("th", {}, "Action"), h("th", {}, "Purpose"), h("th", {}, "Notice"), h("th", {}, "Source"), h("th", {}, "Person"))),
    h("tbody", {}, list.map((r) => h("tr", {}, h("td", {}, r.seq), h("td", {}, when(r.at)), h("td", {}, h("span", { class: "pill" + (r.action === "grant" ? "" : " bad") }, r.action === "grant" ? "Consent" : "Withdrawn")),
      h("td", { class: "mono" }, r.purpose), h("td", {}, r.notice_slug + " v" + r.notice_version + " (" + r.lang + ")"), h("td", {}, r.source), h("td", { class: "mono", title: r.subject }, r.subject.slice(0, 10) + "…")))))) : h("p", { class: "muted" }, "No records."));
  const lookup = async (id) => {
    const q = "?identifier=" + encodeURIComponent(id);
    const [recs, who] = await Promise.all([api("GET", "/admin/records" + q), api("GET", "/admin/subjects" + q)]);
    show(recs.data);
    const link = location.origin + "/manage?token=" + who.manage_token;
    person.replaceChildren(h("div", { class: "card" }, h("h2", {}, "This Person Now"),
      who.consents.length ? h("ul", {}, who.consents.map((c) => h("li", {}, h("b", {}, c.title), ": ", c.granted ? "consent in force" : "withdrawn", " since " + when(c.since), " ",
        c.granted ? h("button", { class: "btn danger small", type: "button", onclick: async () => { if (confirm("Record a withdrawal for " + c.title + " at this person's request?")) { await api("POST", "/admin/withdrawals", { identifier: id, purposes: [c.purpose] }); lookup(id); } } }, "Withdraw") : null))) : h("p", { class: "muted" }, "No consent recorded for this identifier."),
      h("p", { class: "hint" }, "Their private manage link, to include in your emails to them:"), h("pre", {}, link)));
  };
  const verifyOut = h("div", {});
  show((await api("GET", "/admin/records")).data);
  return [h("h1", {}, "Records"), h("p", { class: "muted" }, "Every consent and withdrawal. Records cannot be edited or deleted, and each one is chained to the one before it."),
    h("div", { class: "card" }, form(async (d) => { const id = d.get("identifier").trim(); if (id) await lookup(id); else { person.replaceChildren(); show((await api("GET", "/admin/records")).data); } },
      field("Find a Person", input({ name: "identifier", placeholder: "Email address or other identifier" }), "Identifiers are stored as fingerprints, so search needs the exact value."),
      h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Search"),
        h("button", { class: "btn plain", type: "button", onclick: async () => { const r = await api("GET", "/admin/verify"); verifyOut.replaceChildren(r.ok ? message("Chain intact: " + r.records + " records verified. Head: " + r.head.slice(0, 16) + "…") : message("Chain broken at record " + r.broken_at + ": " + r.problem, true)); } }, "Verify Records"),
        h("a", { class: "btn plain", href: "/api/v1/admin/records.csv" }, "Download CSV"))), verifyOut),
    person, h("div", { class: "card" }, h("h2", {}, "Latest Records"), out)];
}

async function integrations() {
  const [hooks, deliveries, keys] = await Promise.all([api("GET", "/admin/webhooks"), api("GET", "/admin/deliveries"), api("GET", "/admin/keys")]);
  const once = h("div", {});
  const reveal = (title, value) => once.replaceChildren(h("div", { class: "card" }, h("h2", {}, title), h("p", { class: "msg" }, "Copy this now. It is shown only once."), h("pre", {}, value)));
  return [h("h1", {}, "Integrations"), once,
    h("div", { class: "card" }, h("h2", {}, "Webhooks"), h("p", { class: "muted" }, "Sammati sends a signed message to each address whenever consent is given or withdrawn, so your CRM or mailing tool stays in step."),
      hooks.data.length ? h("table", {}, h("tbody", {}, hooks.data.map((w) => h("tr", {}, h("td", { class: "mono" }, w.url), h("td", {}, h("button", { class: "btn danger small", type: "button", onclick: async () => { if (confirm("Remove this webhook?")) { await api("DELETE", "/admin/webhooks/" + w.id); done("Webhook removed."); } } }, "Remove")))))) : null,
      form(async (d, f) => { const w = await api("POST", "/admin/webhooks", { url: d.get("url") }); f.reset(); reveal("Webhook Signing Secret", w.secret); },
        field("Address", input({ name: "url", type: "url", required: true, placeholder: "https://example.com/hooks/consent" })),
        h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Add Webhook")))),
    deliveries.data.length ? h("div", { class: "card scroll" }, h("h2", {}, "Recent Deliveries"), h("table", {}, h("thead", {}, h("tr", {}, h("th", {}, "When"), h("th", {}, "Event"), h("th", {}, "Status"), h("th", {}, "Attempts"), h("th", {}, "Problem"))),
      h("tbody", {}, deliveries.data.map((d) => h("tr", {}, h("td", {}, when(d.created_at)), h("td", { class: "mono" }, d.event), h("td", {}, h("span", { class: "pill" + (d.status === "sent" ? "" : d.status === "failed" ? " bad" : " off") }, d.status)), h("td", {}, d.attempts), h("td", {}, d.last_error)))))) : null,
    h("div", { class: "card" }, h("h2", {}, "API Keys"), h("p", { class: "muted" }, "A key lets your own server use the admin API. Keep keys on servers only, never in a web page or app."),
      keys.data.length ? h("table", {}, h("tbody", {}, keys.data.map((k) => h("tr", {}, h("td", {}, k.name), h("td", { class: "mono" }, k.id), h("td", {}, when(k.created_at)),
        h("td", {}, k.revoked ? h("span", { class: "pill off" }, "Revoked") : h("button", { class: "btn danger small", type: "button", onclick: async () => { if (confirm("Revoke this key? Anything using it stops working.")) { await api("DELETE", "/admin/keys/" + k.id); done("Key revoked."); } } }, "Revoke")))))) : null,
      form(async (d, f) => { const k = await api("POST", "/admin/keys", { name: d.get("name") }); f.reset(); reveal("New API Key", k.key); },
        field("Key Name", input({ name: "name", required: true, maxlength: 80, placeholder: "For example: website backend" })),
        h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Create Key"))))];
}

// logoCard uploads the organisation's logo. It is shown on the admin
// screens and at the top of every consent form and manage page.
function logoCard() {
  const preview = h("div", { class: "logo-preview" }, state.me.has_logo ? h("img", { src: "/logo?v=" + state.logoVersion, alt: "Your logo" }) : h("span", { class: "muted" }, "No logo yet."));
  const err = h("div", {});
  const file = h("input", { type: "file", accept: "image/png,image/jpeg,image/webp", onchange: async () => {
    err.replaceChildren();
    const f = file.files[0];
    if (!f) return;
    if (f.size > 256 * 1024) { err.replaceChildren(message("The image must be 256 KB or smaller.", true)); return; }
    const res = await fetch("/api/v1/admin/logo", { method: "PUT", credentials: "same-origin", headers: { "X-Sammati-Admin": "1", "Content-Type": "application/octet-stream" }, body: f });
    if (!res.ok) { let d = {}; try { d = await res.json(); } catch (e) { /* empty */ } err.replaceChildren(message(d.detail || "The logo could not be saved.", true)); return; }
    state.logoVersion = Date.now(); state.me = await api("GET", "/admin/me"); done("Logo saved.");
  } });
  return [h("p", { class: "muted" }, "Shown at the top of your consent forms and manage pages, beside your organisation name. A wide logo about 160 by 48 pixels works best."),
    preview, h("label", {}, "Upload a Logo", file), h("p", { class: "hint" }, "PNG, JPEG or WebP, up to 256 KB."), err,
    state.me.has_logo ? h("div", { class: "row" }, h("button", { class: "btn danger small", type: "button", onclick: async () => {
      if (!confirm("Remove your logo?")) return;
      await api("DELETE", "/admin/logo"); state.me = await api("GET", "/admin/me"); done("Logo removed.");
    } }, "Remove Logo")) : null];
}

async function settings() {
  const me = state.me.admin;
  const two = h("div", {});
  const twoStep = () => {
    if (me.two_step) return two.replaceChildren(h("p", {}, h("span", { class: "pill" }, "On")), form(async (d) => { await api("POST", "/admin/two-step/disable", { password: d.get("password") }); state.me = await api("GET", "/admin/me"); done("Two-step sign-in switched off."); },
      field("Password", input({ name: "password", type: "password", required: true, autocomplete: "current-password" })), h("div", { class: "row" }, h("button", { class: "btn danger", type: "submit" }, "Switch Off"))));
    two.replaceChildren(h("p", {}, h("span", { class: "pill off" }, "Off"), " Recommended: it protects the admin screens if your password leaks."),
      h("button", { class: "btn", type: "button", onclick: async () => {
        const r = await api("POST", "/admin/two-step/begin");
        two.replaceChildren(h("p", {}, "In your authenticator app, choose to enter a setup key by hand, then type this key:"), h("pre", {}, r.secret.match(/.{1,4}/g).join(" ")),
          h("p", { class: "hint" }, "Account name: " + me.email + ". Type: time-based."),
          form(async (d) => { await api("POST", "/admin/two-step/confirm", { code: d.get("code") }); state.me = await api("GET", "/admin/me"); done("Two-step sign-in is on."); },
            field("Code Shown in the App", input({ name: "code", required: true, inputmode: "numeric", maxlength: 7, autocomplete: "one-time-code" })), h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Confirm"))));
      } }, "Set Up Two-Step Sign-In"));
  };
  twoStep();
  return [h("h1", {}, "Settings"),
    h("div", { class: "card" }, h("h2", {}, "Organisation"), form(async (d) => { await api("PUT", "/admin/settings", { organisation: d.get("organisation") }); state.me = await api("GET", "/admin/me"); done("Saved."); },
      field("Organisation Name", input({ name: "organisation", required: true, maxlength: 120, value: me.organisation })), h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Save")))),
    h("div", { class: "card" }, h("h2", {}, "Your Logo"), logoCard()),
    h("div", { class: "card" }, h("h2", {}, "Two-Step Sign-In"), two),
    h("div", { class: "card" }, h("h2", {}, "Change Password"), form(async (d) => { await api("POST", "/admin/password", { current: d.get("current"), new: d.get("new") }); state.me = null; start(); },
      field("Current Password", input({ name: "current", type: "password", required: true, autocomplete: "current-password" })),
      field("New Password", input({ name: "new", type: "password", required: true, minlength: 12, autocomplete: "new-password" }), "At least 12 characters. You will be signed out everywhere."),
      h("div", { class: "row" }, h("button", { class: "btn", type: "submit" }, "Change Password")))),
    h("div", { class: "card" }, h("h2", {}, "Your Responsibility"), h("p", { class: "note" }, state.me.responsibility))];
}

start();

"use strict";
(async function () {
  const h = window.sammatiEl;
  const app = document.getElementById("app");
  const params = new URLSearchParams(location.search);
  const token = params.get("token") || "";
  const lang = (params.get("lang") || "en").toLowerCase();
  const t = window.sammatiWords(lang);
  document.documentElement.lang = lang;
  document.title = t.manage_title;
  new ResizeObserver(window.sammatiResize).observe(document.body);
  const when = (iso) => new Date(iso).toLocaleDateString(lang, { dateStyle: "long" });

  async function load() {
    app.replaceChildren(h("p", { class: "muted" }, t.loading));
    const res = await fetch("/api/v1/manage?token=" + encodeURIComponent(token));
    if (!res.ok) { app.replaceChildren(h("p", { class: "msg bad" }, t.manage_bad)); return; }
    const data = await res.json();
    const list = data.consents;
    app.replaceChildren(window.sammatiHeader(data.organisation, data.has_logo) || "", h("div", { class: "card" }, h("h1", {}, t.manage_title),
      list.length ? list.map((c) => h("div", { class: "between" },
        h("p", {}, h("b", {}, c.title), h("br"), h("span", { class: "muted" }, (c.granted ? t.in_force : t.withdrawn) + " " + when(c.since))),
        c.granted ? h("button", { class: "btn danger small", type: "button", onclick: async () => {
          if (!confirm(t.withdraw_confirm + " " + c.title + "?")) return;
          const r = await fetch("/api/v1/manage/withdraw", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ token, purposes: [c.purpose] }) });
          if (r.ok) load(); else alert(t.error);
        } }, t.withdraw) : h("span", { class: "pill off" }, t.withdrawn))) : h("p", { class: "muted" }, t.manage_none),
      h("p", { class: "hint" }, t.withdraw_note)), window.sammatiPowered(t));
  }
  load().catch(() => app.replaceChildren(h("p", { class: "msg bad" }, t.error)));
})();

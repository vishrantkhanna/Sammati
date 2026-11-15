"use strict";
(async function () {
  const h = window.sammatiEl;
  const app = document.getElementById("app");
  const params = new URLSearchParams(location.search);
  const id = decodeURIComponent(location.pathname.split("/").pop());
  const lang = (params.get("lang") || document.documentElement.lang || "en").toLowerCase();
  const t = window.sammatiWords(lang);
  document.documentElement.lang = lang;
  if (params.get("embed") === "1") document.body.classList.add("embed");
  // A prefilled identifier comes after #, so it never reaches server logs.
  const prefill = new URLSearchParams(location.hash.slice(1)).get("identifier") || "";
  new ResizeObserver(window.sammatiResize).observe(document.body);

  app.replaceChildren(h("p", { class: "muted" }, t.loading));
  let data;
  try {
    const res = await fetch("/api/v1/forms/" + encodeURIComponent(id) + "?lang=" + encodeURIComponent(lang));
    if (!res.ok) throw new Error();
    data = await res.json();
  } catch (e) { app.replaceChildren(h("p", { class: "msg bad" }, t.unavailable)); return; }

  const { form, notice } = data;
  document.title = form.title;
  const problem = h("div", {});
  const button = h("button", { class: "btn", type: "submit" }, t.agree);
  const identifier = h("input", { type: "text", name: "identifier", required: true, maxlength: 320, value: prefill, autocomplete: "email" });
  // Boxes start unticked: consent has to be a clear action by the person.
  const boxes = form.purposes.map((p) => ({ p, box: h("input", { type: "checkbox", name: p.purpose, required: p.required }) }));

  const el = h("form", { class: "card", onsubmit: async (e) => {
    e.preventDefault(); problem.replaceChildren();
    const chosen = boxes.filter((b) => b.box.checked).map((b) => b.p.purpose);
    if (!chosen.length) { problem.replaceChildren(h("p", { class: "msg bad", role: "alert" }, t.choose)); return; }
    button.disabled = true; button.textContent = t.saving;
    try {
      const res = await fetch("/api/v1/consents", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ form: form.id, identifier: identifier.value, purposes: chosen, lang }) });
      const out = await res.json();
      if (!res.ok) throw new Error(out.detail || t.error);
      const link = location.origin + "/manage?token=" + out.manage_token + (lang !== "en" ? "&lang=" + lang : "");
      app.replaceChildren(window.sammatiHeader(data.organisation, data.has_logo) || "", h("div", { class: "card" }, h("h1", {}, t.saved_title), h("p", {}, form.success_message),
        h("p", { class: "hint" }, t.manage_intro), h("pre", {}, h("a", { href: link, target: "_blank", rel: "noopener" }, link))), window.sammatiPowered(t));
      if (window.parent !== window) window.parent.postMessage({ type: "sammati:consent", form: form.id, purposes: chosen, manage_token: out.manage_token }, "*");
    } catch (ex) {
      problem.replaceChildren(h("p", { class: "msg bad", role: "alert" }, ex.message || t.error));
      button.disabled = false; button.textContent = t.agree;
    }
  } },
    h("h1", {}, form.title),
    data.organisation && data.has_logo ? h("p", { class: "muted" }, t.run_by + ": " + data.organisation) : null,
    h("h2", {}, notice.title), h("p", { class: "notice-body" }, notice.body),
    h("p", { class: "hint" }, t.notice_version + " " + notice.version),
    h("label", {}, form.identifier_label, identifier),
    boxes.map(({ p, box }) => h("label", { class: "check" }, box, h("span", {}, h("b", {}, p.title + " (" + (p.required ? t.required : t.optional) + ")"), p.description))),
    problem, h("div", { class: "row" }, button));
  app.replaceChildren(window.sammatiHeader(data.organisation, data.has_logo) || "", el, window.sammatiPowered(t));
})();

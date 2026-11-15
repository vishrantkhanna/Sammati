/* Sammati website widget.
 *
 *   <script src="https://consent.example.com/widget.js" data-form="newsletter"></script>
 *
 * Optional attributes: data-lang="hi", data-identifier="asha@example.com"
 * (prefills the field), data-target="#some-element" (where to put the form;
 * by default it appears where the script tag is).
 *
 * When a person submits the form, the page receives an event:
 *   document.addEventListener("sammati:consent", (e) => console.log(e.detail));
 */
(function () {
  "use strict";
  var script = document.currentScript;
  if (!script || !script.dataset.form) return;
  var origin = new URL(script.src).origin;
  var src = origin + "/f/" + encodeURIComponent(script.dataset.form) + "?embed=1" +
    (script.dataset.lang ? "&lang=" + encodeURIComponent(script.dataset.lang) : "") +
    (script.dataset.identifier ? "#identifier=" + encodeURIComponent(script.dataset.identifier) : "");
  var frame = document.createElement("iframe");
  frame.src = src;
  frame.title = "Consent form";
  frame.loading = "lazy";
  frame.style.cssText = "width:100%;max-width:560px;border:0;display:block;height:520px;color-scheme:light";
  var target = script.dataset.target && document.querySelector(script.dataset.target);
  if (target) target.appendChild(frame); else script.parentNode.insertBefore(frame, script);
  window.addEventListener("message", function (e) {
    if (e.origin !== origin || e.source !== frame.contentWindow || !e.data) return;
    if (e.data.type === "sammati:height" && e.data.height > 0) frame.style.height = Math.min(e.data.height + 8, 4000) + "px";
    if (e.data.type === "sammati:consent") document.dispatchEvent(new CustomEvent("sammati:consent", { detail: { form: e.data.form, purposes: e.data.purposes, manageToken: e.data.manage_token } }));
  });
})();

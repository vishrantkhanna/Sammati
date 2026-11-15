"use strict";
// Words for the pages people see. Add a language by adding a block here.
window.SAMMATI_WORDS = {
  en: {
    loading: "Loading…", unavailable: "This form is not available.", required: "Required", optional: "Optional",
    agree: "I Agree", saving: "Saving…", choose: "Choose at least one option to continue.", error: "Something went wrong. Please try again.",
    saved_title: "Saved", manage_intro: "Keep this private link. You can use it at any time to see or withdraw your consent:",
    manage_title: "Your Consent", manage_none: "There is no consent on record for this link.", manage_bad: "This link is not valid.",
    in_force: "You agreed on", withdrawn: "Withdrawn on", withdraw: "Withdraw", withdraw_confirm: "Withdraw your consent for",
    withdraw_note: "Withdrawing is as simple as agreeing. It takes effect at once and does not affect what was done before.",
    notice_version: "Notice version", run_by: "Asked by", powered: "Powered by Sammati",
  },
  hi: {
    loading: "लोड हो रहा है…", unavailable: "यह फ़ॉर्म उपलब्ध नहीं है।", required: "आवश्यक", optional: "वैकल्पिक",
    agree: "मैं सहमत हूँ", saving: "सहेजा जा रहा है…", choose: "आगे बढ़ने के लिए कम से कम एक विकल्प चुनें।", error: "कुछ गड़बड़ हुई। कृपया फिर से प्रयास करें।",
    saved_title: "सहेज लिया गया", manage_intro: "यह निजी लिंक संभालकर रखें। इससे आप कभी भी अपनी सहमति देख या वापस ले सकते हैं:",
    manage_title: "आपकी सहमति", manage_none: "इस लिंक के लिए कोई सहमति दर्ज नहीं है।", manage_bad: "यह लिंक मान्य नहीं है।",
    in_force: "आपने सहमति दी", withdrawn: "वापस ली गई", withdraw: "सहमति वापस लें", withdraw_confirm: "क्या आप सहमति वापस लेना चाहते हैं:",
    withdraw_note: "सहमति वापस लेना उतना ही आसान है जितना देना। यह तुरंत लागू होता है और पहले किए गए कार्यों पर असर नहीं डालता।",
    notice_version: "सूचना संस्करण", run_by: "अनुरोधकर्ता", powered: "Sammati द्वारा संचालित",
  },
};
window.sammatiWords = function (lang) { return Object.assign({}, window.SAMMATI_WORDS.en, window.SAMMATI_WORDS[lang] || {}); };
window.sammatiEl = function (tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v == null) continue;
    if (k === "class") el.className = v; else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "checked" || k === "disabled" || k === "required") el[k] = !!v; else if (k === "value") el.value = v; else el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) { if (kid != null && kid !== false) el.append(kid.nodeType ? kid : document.createTextNode(String(kid))); }
  return el;
};
// The organisation's logo and name, shown above the form.
window.sammatiHeader = function (organisation, hasLogo) {
  const h = window.sammatiEl;
  if (!hasLogo && !organisation) return null;
  return h("div", { class: "org-head" }, hasLogo ? h("img", { src: "/logo", alt: organisation || "" }) : null, hasLogo ? null : h("b", {}, organisation));
};
// A small line that says the form runs on Sammati, with its mark.
window.sammatiPowered = function (t) {
  const h = window.sammatiEl;
  return h("p", { class: "powered" }, h("a", { href: "https://github.com/vishrantkhanna/sammati", target: "_blank", rel: "noopener" },
    h("img", { src: "/static/mark.svg", alt: "", width: 14, height: 14 }), t.powered));
};
// Tell a page that embeds this one how tall it is, so the frame can fit.
window.sammatiResize = function () {
  if (window.parent !== window) window.parent.postMessage({ type: "sammati:height", height: document.documentElement.scrollHeight }, "*");
};

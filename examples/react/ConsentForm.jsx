import { useEffect, useRef } from "react";

// Shows a Sammati consent form.
//
//   <ConsentForm server="https://consent.example.com" form="newsletter"
//                email={user.email} onConsent={(d) => console.log(d.purposes)} />
export function ConsentForm({ server, form, lang, email, onConsent }) {
  const holder = useRef(null);

  useEffect(() => {
    const node = holder.current;
    const script = document.createElement("script");
    script.src = `${server}/widget.js`;
    script.dataset.form = form;
    if (lang) script.dataset.lang = lang;
    if (email) script.dataset.identifier = email;
    node.append(script);
    return () => node.replaceChildren();
  }, [server, form, lang, email]);

  useEffect(() => {
    if (!onConsent) return undefined;
    const handler = (e) => { if (e.detail.form === form) onConsent(e.detail); };
    document.addEventListener("sammati:consent", handler);
    return () => document.removeEventListener("sammati:consent", handler);
  }, [form, onConsent]);

  return <div ref={holder} />;
}

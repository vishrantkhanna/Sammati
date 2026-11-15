# Embed on a Website

## One Line

Paste this where the form should appear. Replace the address with your
Sammati address and `newsletter` with your form's name.

```html
<script src="https://consent.example.com/widget.js" data-form="newsletter"></script>
```

**Forms** in the admin screens shows the exact line for each form under
**Put It on Your Website**.

## Options

| Attribute | Does |
| --- | --- |
| `data-form` | The form's name. Required. |
| `data-lang` | Language of the notice and buttons, for example `hi`. |
| `data-identifier` | Prefills the field, for a signed-in user's email. |
| `data-target` | A CSS selector for where to put the form. |

## React to a Submission

```html
<script>
  document.addEventListener("sammati:consent", function (e) {
    // e.detail = { form, purposes, manageToken }
    console.log("Consent recorded for", e.detail.purposes);
  });
</script>
```

Save `manageToken` with the user's account if you want to show them a
"manage my consent" link later: `https://consent.example.com/manage?token=...`.

## Just a Link

Every form also has its own page, for emails, QR codes and chat:

```
https://consent.example.com/f/newsletter
https://consent.example.com/f/newsletter?lang=hi
```

## Your Own Form

If you would rather design the form yourself, call the public API from
your page. See [API Reference](api.md) and `examples/html`.

Show the notice text from the API, start every box unticked, and send only
the purposes the person ticked.

## Limit Which Sites May Embed

By default any site can embed the form. To allow only yours, set:

```
SAMMATI_FRAME_ANCESTORS=https://www.example.com https://shop.example.com
SAMMATI_ALLOW_ORIGIN=https://www.example.com
```

## More Examples

See `examples/` for plain HTML, React and WordPress.

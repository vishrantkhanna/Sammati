# Webhooks

A webhook tells your own systems the moment consent is given or withdrawn,
so your mailing tool or CRM never acts on stale consent.

## Add One

**Integrations**, then **Add Webhook**. The signing secret is shown once.
Store it with the receiving system.

## What Is Sent

A `POST` with a JSON body:

```json
{
  "event": "consent.withdrawn",
  "created_at": "2026-11-15T06:10:11.123Z",
  "subject": "9f2c...",
  "form_id": "newsletter",
  "source": "manage",
  "records": [{"seq": 13, "action": "withdraw", "purpose": "newsletter", "...": "..."}]
}
```

Events are `consent.granted` and `consent.withdrawn`.

The body carries the person's fingerprint (`subject`), never their email.
To match it to your users, save the fingerprint when you record consent:
the reply to `POST /consents` and `GET /admin/subjects?identifier=` both
include it.

## Check the Signature

Every delivery has a header:

```
X-Sammati-Signature: t=1794705105,v1=5f2b...
```

`v1` is the HMAC-SHA256, in hex, of `t`, a dot, and the exact body, keyed
with your signing secret. Recompute it and compare. Reject the delivery if
it differs or if `t` is more than five minutes old. Working receivers in
Node and Python are in `examples/webhook-receiver`.

## Retries

Answer with any 2xx status within ten seconds. Otherwise Sammati tries
again after 1 minute, 5 minutes, 30 minutes, 2 hours and 12 hours, then
marks the delivery failed. **Integrations** lists recent deliveries and
the reason for any failure.

A delivery can arrive more than once. Use the `X-Sammati-Delivery` header
to ignore repeats.

## Note

Sammati sends to whatever address an administrator enters, including
addresses inside your own network. Only administrators can add webhooks.

# API Reference

Base address: `https://your-sammati/api/v1`. Bodies and replies are JSON.

Errors look like this, with `field` present for input problems:

```json
{"error": "Unprocessable Content", "field": "purposes", "detail": "purposes choose at least one"}
```

## Public API

No key. Safe to call from a web page. Limited to 60 requests a minute per
address.

### Show a Form

`GET /forms/{id}?lang=hi`

Returns the form, its purposes, the organisation name and the newest
notice in that language (English when the language was not published).

### Record Consent

`POST /consents`

```json
{"form": "newsletter", "identifier": "asha@example.com", "purposes": ["newsletter"], "lang": "en"}
```

- `purposes` are the ones the person ticked. Every required purpose of the
  form must be included.
- The reply has the new `records` and a `manage_token` for this person.
- The identifier is never stored or returned. Sammati keeps a keyed
  fingerprint of it.

### See Consents

`GET /manage?token=TOKEN`

### Withdraw

`POST /manage/withdraw`

```json
{"token": "TOKEN", "purposes": ["newsletter"]}
```

## Admin API

Send an API key from your server: `Authorization: Bearer smt_...`. Create
keys under **Integrations**, or with `sammati key create NAME`. Never put a
key in a web page or mobile app.

| Method and Path | Does |
| --- | --- |
| `GET /admin/purposes` | List purposes |
| `POST /admin/purposes` | Create: `code`, `title`, `description` |
| `POST /admin/purposes/{code}/archive` | Stop offering a purpose on new forms |
| `GET /admin/notices` | Every version of every notice |
| `POST /admin/notices` | Publish: `slug`, `texts: [{lang, title, body}]` (English required) |
| `GET /admin/forms` | List forms |
| `POST /admin/forms` | Create: `id`, `title`, `notice_slug`, `purposes: [{purpose, required}]`, optional `identifier_label`, `success_message` |
| `POST /admin/forms/{id}/active` | Switch on or off: `{"active": false}` |
| `POST /admin/consents` | Record consent your own system collected (same body as the public call; source is stored as `api`) |
| `POST /admin/withdrawals` | Withdraw at a person's request: `identifier`, `purposes` |
| `GET /admin/subjects?identifier=` | One person's current consents and manage token |
| `GET /admin/records?identifier=&purpose=&before=&limit=` | Records, newest first. Page with `before` = the lowest `seq` you have |
| `GET /admin/records.csv` | Every record |
| `GET /admin/verify` | Check the chain: `{ok, records, head}` |
| `GET /admin/stats` | Counts for a dashboard |
| `GET, POST /admin/webhooks`, `DELETE /admin/webhooks/{id}` | Manage webhooks |
| `GET /admin/deliveries` | The latest 50 webhook deliveries |
| `GET, POST /admin/keys`, `DELETE /admin/keys/{id}` | Manage API keys |

## Check Before You Send

The usual use from a backend: before sending a marketing email, ask
whether consent is in force.

```sh
curl -s "https://your-sammati/api/v1/admin/subjects?identifier=asha@example.com" \
  -H "Authorization: Bearer $SAMMATI_KEY"
```

Send only if the purpose is listed with `"granted": true`.

## The Record

```json
{"seq": 12, "at": "2026-11-15T06:10:11.123Z", "subject": "9f2c...", "action": "grant",
 "purpose": "newsletter", "notice_slug": "newsletter", "notice_version": 2, "lang": "en",
 "form_id": "newsletter", "source": "form", "prev_hash": "...", "hash": "..."}
```

`hash` is the SHA-256 of these fields, one per line, each followed by a
newline: `prev_hash`, `seq`, `at`, `subject`, `action`, `purpose`,
`notice_slug`, `notice_version`, `lang`, `form_id`, `source`. The first
record's `prev_hash` is 64 zeros. Anyone can recompute the chain from the
CSV export.

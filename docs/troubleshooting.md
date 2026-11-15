# Troubleshooting

## Locked Out of the Admin Screens

Run this on the server. It sets a new password, switches two-step
sign-in off and signs everyone out:

```sh
docker compose exec -T sammati /sammati admin reset-password <<< 'a new long password'
```

Without Docker:

```sh
echo 'a new long password' | SAMMATI_DB=/var/lib/sammati/sammati.db sammati admin reset-password
```

The sign-in page gives no hints about this on purpose, so that visitors
learn nothing about your installation.

## Lost Your Authenticator App

Use the reset above, then set up two-step sign-in again under
**Settings**.

## The Form Will Not Appear on My Site

- Check the address in the script tag is your Sammati address, with
  HTTPS.
- If you set `SAMMATI_FRAME_ANCESTORS`, your site must be in it.
- Open the browser console on your site and look for a blocked frame.

## "Too Many Requests"

The public API allows 60 requests a minute from one address, and sign-in
allows 10. Behind a reverse proxy, set `SAMMATI_TRUST_PROXY=1`, or every
visitor looks like the proxy's address and shares one limit.

## Webhooks Are Not Arriving

Open **Integrations** and read **Recent Deliveries**. "Could not connect"
means the address cannot be reached from the Sammati server. "The receiver
answered 401" usually means the receiver's signature check is using the
wrong secret.

## Verify Says the Chain Is Broken

Do not change anything. Restore your latest backup into a separate folder
and run `sammati verify` on it, to find when the change happened. A broken
chain means the database file was edited outside Sammati.

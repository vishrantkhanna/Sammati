# Security Checklist

You run this installation, so its security is yours to look after. Work
through this list before you collect real consent.

## Before Going Live

- [ ] Served only over HTTPS, through a reverse proxy you control
- [ ] The port is not reachable directly from the internet (`SAMMATI_BIND`
      left at `127.0.0.1`)
- [ ] Setup completed straight after install, with a long, unique password
- [ ] Two-step sign-in switched on under **Settings**
- [ ] `SAMMATI_ALLOW_ORIGIN` and `SAMMATI_FRAME_ANCESTORS` set to your own
      sites
- [ ] `SAMMATI_TRUST_PROXY=1` set only because a proxy you control is the
      only way in
- [ ] Server firewall on, operating system updates automatic

## Keys and Secrets

- [ ] API keys used only from servers, never in web pages or apps
- [ ] One key per system, revoked when no longer needed
- [ ] Webhook receivers check the signature

## Data

- [ ] Daily encrypted backups kept on another machine
- [ ] A restore has been tested
- [ ] Access to the server and to backups limited to named people
- [ ] The chain head from `sammati verify` noted somewhere separate from
      time to time

## Your Notice and Forms

- [ ] Each purpose is specific and separate
- [ ] Only what the service truly needs is marked Required
- [ ] The notice says what you collect, why, for how long, how to withdraw
      and how to complain
- [ ] The manage link goes out in your confirmation email

## What Sammati Does for You

- Stores identifiers only as keyed fingerprints
- Makes records append-only and tamper-evident
- Hashes the admin password and API keys
- Limits request rates on public and sign-in endpoints
- Sends no data anywhere except the webhooks you add

## What It Cannot Do

It cannot secure your server, write your notice, or decide whether consent
is the right basis for what you do. Ask a qualified adviser about your
legal duties.

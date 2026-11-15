# Security

## Reporting a Problem

Please report security problems privately through GitHub's "Report a
vulnerability" button on this repository. Do not open a public issue.
You will get a reply within seven days.

## Your Responsibilities as the Operator

Sammati is self-hosted. The person or organisation that runs it is
responsible for:

- Serving it only over HTTPS, behind a reverse proxy you control.
- Keeping the database file private and backed up. It holds the keys that
  turn identifiers into fingerprints, so treat it as a secret.
- Keeping admin API keys out of web pages and public repositories.
- Applying updates.
- Meeting the data protection law that applies to you.

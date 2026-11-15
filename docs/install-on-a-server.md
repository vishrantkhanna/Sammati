# Install on a Server

Sammati must be served over HTTPS in real use. Run it on the server, bound
to the machine itself, and put a reverse proxy in front.

## 1. Start Sammati

```sh
git clone https://github.com/vishrantkhanna/sammati
cd sammati
SAMMATI_TRUST_PROXY=1 docker compose up -d --build
```

It listens on `127.0.0.1:8787` only.

## 2. Add HTTPS

### Caddy

Caddy gets and renews the certificate by itself. `deploy/Caddyfile`:

```
consent.example.com {
    reverse_proxy 127.0.0.1:8787
}
```

### Nginx

See `deploy/nginx.conf`. Use certbot or your own certificate.

## 3. Finish Setup

Open `https://consent.example.com` and complete the setup screen straight
away. Until you do, anyone who finds the address could claim the admin
account. Then switch on two-step sign-in under **Settings**.

## Settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `SAMMATI_DB` | `/data/sammati.db` in Docker | Database file |
| `SAMMATI_ADDR` | `0.0.0.0:8787` in Docker | Listen address inside the container |
| `SAMMATI_BIND` | `127.0.0.1` | Host address the port is published on (Compose) |
| `SAMMATI_PORT` | `8787` | Host port (Compose) |
| `SAMMATI_ALLOW_ORIGIN` | `*` | Sites allowed to call the public API |
| `SAMMATI_FRAME_ANCESTORS` | `*` | Sites allowed to embed the form |
| `SAMMATI_TRUST_PROXY` | off | `1` behind your own proxy: reads the visitor's address and HTTPS state from the proxy's headers |

Set `SAMMATI_TRUST_PROXY=1` only when a proxy you control is the only way
in. Otherwise visitors could fake their address and dodge the rate limit.

## Without Docker

Build the program (`make build`), copy it to `/usr/local/bin/sammati`,
create a `sammati` user and a `/var/lib/sammati` folder it owns, then use
`deploy/sammati.service` with systemd.

## Next

Work through the [Security Checklist](security-checklist.md) and set up
[backups](backup-and-restore.md).

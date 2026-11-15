# Quick Start

You need a machine with Docker and the Compose plugin.

```sh
git clone https://github.com/vishrantkhanna/sammati
cd sammati
docker compose up -d --build
```

Open <http://localhost:8787>. The first visit shows the setup screen:

1. Enter your organisation name, your email and a password of at least 12
   characters.
2. Read the responsibility statement and tick the box.
3. Choose **Finish Setup**.

You are now signed in. Go to [Create Your First Form](first-form.md).

## Testing From Another Machine

By default the port only answers on the machine itself. On a test machine
you reach over the network, start it like this instead:

```sh
SAMMATI_BIND=0.0.0.0 docker compose up -d --build
```

Then open `http://<the machine's address>:8787`. Do not do this on the
public internet without HTTPS. See [Install on a Server](install-on-a-server.md).

## Without Docker

With Go 1.27 or later:

```sh
go mod tidy
go build -o sammati ./cmd/sammati
./sammati serve
```

## Useful Commands

```sh
docker compose logs -f                      # watch the log
docker compose exec sammati /sammati verify # check the record chain
docker compose down                         # stop (your data is kept)
```

## Locked Out

```sh
docker compose exec -T sammati /sammati admin reset-password <<< 'a new long password'
```

This sets a new password, switches two-step sign-in off and signs everyone
out. See [Troubleshooting](troubleshooting.md).

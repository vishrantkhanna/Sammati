# Backup and Restore

Everything is in one database file, plus two small side files while the
server runs. The file also holds the keys that turn identifiers into
fingerprints. If you lose it, you lose your proof of consent, and nobody
can rebuild it.

## Back Up (Docker)

Stop, copy, start. It takes a few seconds.

```sh
docker compose stop
docker run --rm -v sammati_sammati-data:/data -v "$PWD":/backup alpine \
  tar -czf /backup/sammati-$(date +%F).tar.gz -C /data .
docker compose start
```

The volume is named after the folder you cloned into. Check the name with
`docker volume ls`.

## Back Up (Without Docker)

```sh
systemctl stop sammati
cp /var/lib/sammati/sammati.db* /your/backup/folder/
systemctl start sammati
```

## Restore

```sh
docker compose down
docker run --rm -v sammati_sammati-data:/data -v "$PWD":/backup alpine \
  sh -c "rm -rf /data/* && tar -xzf /backup/sammati-2026-11-15.tar.gz -C /data && chown -R 65532:65532 /data"
docker compose up -d
docker compose exec sammati /sammati verify
```

## Good Practice

- Back up every day, and keep copies on another machine.
- Encrypt the backups. They are as sensitive as the live file.
- After each backup, note the `Head` value printed by `sammati verify`
  somewhere separate. If the records are ever questioned, a matching head
  shows the chain is the one you had on that day.
- Test a restore before you need one.

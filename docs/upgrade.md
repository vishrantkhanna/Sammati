# Upgrade

1. Read the [changelog](../CHANGELOG.md) for the version you are moving to.
2. [Back up](backup-and-restore.md).
3. Get the new version and rebuild:

```sh
git pull
docker compose up -d --build
docker compose exec sammati /sammati verify
```

Sammati updates its own database when it starts. Your records, notices
and settings are kept.

To go back, restore the backup and check out the earlier version.

# Security

AI Usage Dashboard is designed as a local-first observability tool.

## Data Handling

- Source log/session mounts should be read-only.
- The app stores parsed usage metadata in SQLite under `data/`.
- Do not publish `data/usage.db`; it may contain project paths, model names, timestamps, and usage metadata.
- The collector does not need provider credentials, cookies, or API keys.

## Deployment

Do not expose this service directly to the public internet without authentication and TLS. The MVP has no built-in auth because it is intended for localhost or trusted private networks.

## Reporting

If you find a security issue, open a private report or contact the repository owner directly. Avoid posting sensitive logs or database files in public issues.


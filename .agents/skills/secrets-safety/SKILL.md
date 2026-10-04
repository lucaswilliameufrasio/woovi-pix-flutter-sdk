---
name: secrets-safety
description: Use when handling credentials, environment configuration, payment-provider keys, bearer tokens, database connections, CI secrets, or a suspected leak. Prevent secret disclosure in public files, logs, tool output, and commits.
---

# Secrets Safety

## Keep resolved secrets out of output

- Do not print, log, echo, or commit credentials and resolved secret values.
- Avoid commands that dump environment variables, secret stores, or configuration files containing credentials. Inspect names, schemas, references, or redacted metadata instead.
- Do not reproduce secrets already seen in a conversation, log, or error response.
- Use clearly fictional placeholders in documentation and configuration examples. Public local-demo credentials must be explicitly test-only and never reused elsewhere.
- Keep provider credentials on the backend. Never embed them in Flutter applications, source files, build artifacts, or public SDK examples.
- A test-only session token embedded in an example application is not production authentication; do not distribute an artifact containing a real token.

## Configure without disclosure

- Prefer secret references between services, protected CI secret stores, or write-only input through an approved tool.
- Read interactive credentials without terminal echo and without literal values in shell history.
- Verify operation through health checks, status, test results, or redacted logs rather than by displaying the configured value.
- Do not initiate credential rotation, permission changes, or infrastructure mutations without authorization.

## Respond to exposure

1. Stop reproducing the value.
2. Notify the user of the affected location and scope without quoting the secret.
3. Recommend revocation/rotation through the provider's documented procedure.
4. Remove the value from pending changes when authorized; do not rewrite Git history on your own initiative.
5. Record an incident only in an approved destination, using redacted evidence.

## Review before delivery

- Check staged changes, copied resources, documentation, fixtures, and scripts for credentials and private paths.
- Treat `.env` files, provider keys, access/refresh tokens, private keys, and database URLs as potentially sensitive.
- Do not promise that deleting a public file undoes exposure; published credentials may already have been copied.
- Preserve public licensing and author attribution when copying reusable material, but remove private project context.

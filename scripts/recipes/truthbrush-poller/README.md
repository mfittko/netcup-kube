# truthbrush-poller recipe

Deploys a low-latency Truth poller as a Kubernetes `Deployment`.

The poller:
- uses [truthbrush](https://github.com/stanfordio/truthbrush) against Truth Social,
- stores minimal state in SQLite (`/data/state.db` on a PVC),
- posts only new posts to your OpenClaw webhook URL.

## Install

```bash
TRUTHSOCIAL_TOKEN="<truthsocial-token>" \
netcup-kube install truthbrush-poller \
  --namespace openclaw \
  --webhook-url "https://<openclaw-host>/hooks/truthbrush"
```

With username/password login and webhook auth:

```bash
TRUTHSOCIAL_USERNAME="<truthsocial-username>" \
TRUTHSOCIAL_PASSWORD="<truthsocial-password>" \
OPENCLAW_WEBHOOK_BEARER="<token>" \
OPENCLAW_WEBHOOK_SIGNING_KEY="<hmac-key>" \
netcup-kube install truthbrush-poller \
  --webhook-url "https://<openclaw-host>/hooks/truthbrush"
```

Use an existing secret instead of creating one:

```bash
netcup-kube install truthbrush-poller \
  --secret-name truthbrush-runtime-secrets \
  --use-existing-secret
```

Expected secret keys:
- `truthsocial-token` (or both `truthsocial-username` + `truthsocial-password`)
- `webhook-url`
- `webhook-bearer` (optional)
- `webhook-signing-key` (optional)

## Common options

- `--poll-seconds` (default `20`)
- `--target-handle` (default `realDonaldTrump`)
- `--secret-name` (default `<name>-secrets`)
- `--use-existing-secret` (default `false`)
- `--state-size` (default `1Gi`)
- `--name` (default `truthbrush-poller`)

## Payload contract

The webhook receives JSON:

```json
{
  "source": "truthbrush-poller",
  "event": "truthsocial.new_post",
  "generatedAt": "2026-03-03T21:34:56Z",
  "idempotencyKey": "truthsocial:114291234567890123",
  "post": {
    "id": "114291234567890123",
    "url": "https://truthsocial.com/@realDonaldTrump/posts/114291234567890123",
    "createdAt": "2026-03-03T20:34:00.000Z",
    "content": "..."
  }
}
```

Headers:
- `X-Idempotency-Key: truthsocial:<post-id>`
- `X-Truthbrush-Event: truthsocial.new_post`
- `Authorization: Bearer <token>` (if configured)
- `X-Truthbrush-Signature: sha256=<hex>` (if signing key configured)

## Uninstall

```bash
netcup-kube install truthbrush-poller --uninstall
```

Delete secret as well:

```bash
netcup-kube install truthbrush-poller --uninstall --delete-secret
```# truthbrush-poller recipe

Deploys a low-latency Truth poller as a Kubernetes `Deployment`.

The poller:
- uses [truthbrush](https://github.com/stanfordio/truthbrush) against Truth Social,
- stores minimal state in SQLite (`/data/state.db` on a PVC),
- posts only new posts to your OpenClaw webhook URL.

## Install

```bash
TRUTHSOCIAL_TOKEN="<truthsocial-token>" \
netcup-kube install truthbrush-poller \
  --namespace openclaw \
  --webhook-url "https://<openclaw-host>/hooks/truthbrush"
```

With username/password login and webhook auth:

```bash
TRUTHSOCIAL_USERNAME="<truthsocial-username>" \
TRUTHSOCIAL_PASSWORD="<truthsocial-password>" \
OPENCLAW_WEBHOOK_BEARER="<token>" \
OPENCLAW_WEBHOOK_SIGNING_KEY="<hmac-key>" \
netcup-kube install truthbrush-poller \
  --webhook-url "https://<openclaw-host>/hooks/truthbrush"
```

Use an existing secret instead of creating one:

```bash
netcup-kube install truthbrush-poller \
  --secret-name truthbrush-runtime-secrets \
  --use-existing-secret
```

Expected secret keys:
- `truthsocial-token` (or both `truthsocial-username` + `truthsocial-password`)
- `webhook-url`
- `webhook-bearer` (optional)
- `webhook-signing-key` (optional)

## Common options

- `--poll-seconds` (default `20`)
- `--target-handle` (default `realDonaldTrump`)
- `--secret-name` (default `<name>-secrets`)
- `--use-existing-secret` (default `false`)
- `--state-size` (default `1Gi`)
- `--name` (default `truthbrush-poller`)

## Payload contract

The webhook receives JSON:

```json
{
  "source": "truthbrush-poller",
  "event": "truthsocial.new_post",
  "generatedAt": "2026-03-03T21:34:56Z",
  "idempotencyKey": "truthsocial:114291234567890123",
  "post": {
    "id": "114291234567890123",
    "url": "https://truthsocial.com/@realDonaldTrump/posts/114291234567890123",
    "createdAt": "2026-03-03T20:34:00.000Z",
    "content": "..."
  }
}
```

Headers:
- `X-Idempotency-Key: truthsocial:<post-id>`
- `X-Truthbrush-Event: truthsocial.new_post`
- `Authorization: Bearer <token>` (if configured)
- `X-Truthbrush-Signature: sha256=<hex>` (if signing key configured)

## Uninstall

```bash
netcup-kube install truthbrush-poller --uninstall
```

Delete secret as well:

```bash
netcup-kube install truthbrush-poller --uninstall --delete-secret
```

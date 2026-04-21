# multi-cloud

AWS + GCP + a secret-manager-sourced Cloudflare token, all bound per-stack.
Shows:

- Multiple federated providers on the same stack (union).
- `override:` for a payments stack that uses a tighter AWS role.
- `mode: drift` bindings for read-only drift credentials.
- Secret-manager provider (`aws_secrets_manager`) for non-IAM credentials.
- GitHub App for PR comments (higher rate limits than `GITHUB_TOKEN`).

See [aws-oidc/README.md](../aws-oidc/README.md) and
[gcp-wif/README.md](../gcp-wif/README.md) for the one-time cloud setup.

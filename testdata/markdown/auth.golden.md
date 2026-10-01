# Authentication

The Acme API uses OAuth 2.0 bearer tokens. Send the token in the `Authorization` header of every request.

## Obtaining a token

Exchange your client credentials at the token endpoint:

```bash
curl -X POST https://api.acme.test/oauth/token \
  -d grant_type=client_credentials \
  -d client_id="$ACME_CLIENT_ID" \
  -d client_secret="$ACME_CLIENT_SECRET"
```

The response contains a `access_token` valid for one hour.

## Rotating keys

Keys can overlap. Create the new key before revoking the old one to avoid downtime.

- Keys are scoped per environment.
- Revoking a key takes effect within 60 seconds.

## API keys

Static API keys work for server-to-server calls. See the [API reference](https://docs.acme.test/latest/api) for header formats.

| Header | Value |
|---|---|
| Authorization | Bearer &lt;token&gt; |
| X-Acme-Key | &lt;static-key&gt; |

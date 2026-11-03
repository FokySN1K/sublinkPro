English | [简体中文](happ-link.zh-CN.md)

# happ:// Links and the Decryptor

happ links (`happ://crypt5/...`) hide the real subscription address behind server-side encryption. SublinkPro can decode every published generation locally and use the resulting plaintext URL as a normal subscription address.

## Supported generations

| Link type | Payload |
|---|---|
| `happ://crypt/...` | RSA PKCS#1 v1.5, first generation |
| `happ://crypt2/...` | RSA PKCS#1 v1.5, second generation |
| `happ://crypt3/...` | RSA PKCS#1 v1.5, third generation |
| `happ://crypt4/...` | RSA PKCS#1 v1.5, fourth generation |
| `happ://crypt5/...` | RSA key recovery plus ChaCha20-Poly1305 |

All private keys are compiled into the SublinkPro binary and the crypt5 key table is loaded once per process on first use. Decryption is fully offline: no request leaves your server, and neither the link nor the result is reported anywhere.

## Using happ links with airports

1. Open **Subscription Management → Airport** and add or edit an airport.
2. Paste the `happ://` link into **Subscription URL** and save.

The link is decrypted before validation and before it is stored, so the airport record keeps the plaintext subscription URL. If the link cannot be decrypted, the airport is not saved and you get a localized error such as "Unable to decrypt the happ link" instead of a broken record.

Existing records are handled at runtime as well: subscription pulls (`LoadClashConfigFromURLWithReporter`) and usage refreshes resolve a `happ://` value on the fly, so an airport that already stores a link keeps working without manual editing.

## Decryptor page

**Tools → happ Decryptor** (`/tools/happ`) decodes a link and shows:

- the plaintext URL, with a copy button;
- the detected link type (`crypt` … `crypt5`);
- an "Open" button when the result is an `http(s)` address.

The page never sends the link anywhere except your own SublinkPro backend.

## REST API

```bash
curl -X POST "$BASE/api/v1/tools/happ/decrypt" \
  -H "X-API-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"link":"happ://crypt5/..."}'
```

Successful response:

```json
{
  "code": 200,
  "msg": "happ link decrypted",
  "data": {
    "url": "https://example.com/sub?token=...",
    "generation": "crypt5"
  },
  "i18nKey": "backend.happ.decryptSuccess"
}
```

Failure responses keep the standard envelope with `code: 500` and an `i18nKey` describing the problem:

| `i18nKey` | Meaning |
|---|---|
| `backend.happ.linkRequired` | The request body has no link |
| `backend.happ.invalidLink` | The value does not start with `happ://` |
| `backend.happ.unsupportedLink` | Unknown link type, missing payload, or an unknown crypt5 marker |
| `backend.happ.decryptFailed` | The payload is truncated, modified, or encrypted with a key this build does not know |

## Limits and notes

- A link may be at most 64 KiB of text; longer values are rejected.
- Only the link types listed above are supported. A future generation returns `backend.happ.unsupportedLink` until the key table is updated, so upgrade SublinkPro when a new generation appears.
- Because the plaintext address is stored in the airport record after saving, treat the airport row as a plaintext subscription URL: restrict it with the normal role and API-key permissions.
- The decryptor does not validate or whitelist the target host. Only decode links from sources you trust; fetching afterwards uses the normal airport request settings (User-Agent, custom headers, proxy download, skip TLS verify).

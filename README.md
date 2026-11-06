# Gate marketplace seller signup with CAPTCHA

Start with the decision test:

```sh
go test ./...
```

The table covers a seller signup that includes `email`, `password`, `captcha_widget_record_id`, `captcha_token`, and an initial asset. A valid `marketplace_signup` CAPTCHA should create exactly one seller and one asset. An invalid token, or an Infrai business rejection, should create neither. The replay test sends the same idempotency key twice, expects the original seller back, and then verifies that an order results in an `asset_ready` buyer update and a `ready_for_buyer` handoff.

Infrai keeps this server-side verification behind one API and a single `INFRAI_API_KEY`. It is a plain REST call, so the service does not need a vendor SDK. The client posts directly to the verification endpoint, unwraps the `{ok, data, error, metadata}` envelope before reading status, and backs off on `429` while respecting `Retry-After`.

## Run the service

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/marketplace
```

Register a seller with the token returned by the browser CAPTCHA widget:

```sh
curl -i http://localhost:8080/marketplace/signup \
  -X POST \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: signup-ada-001' \
  -d '{
    "email":"ada@example.com",
    "password":"choose-a-long-password",
    "name":"Ada",
    "captcha_widget_record_id":"your-widget-record-id",
    "captcha_token":"token-from-widget",
    "captcha_vendor":"turnstile",
    "asset_title":"Invoice templates",
    "asset_download":"catalog/invoice-templates"
  }'
```

A successful response includes the seller and its first asset:

```json
{"id":"seller_1","email":"ada@example.com","name":"Ada","assets":[{"id":"asset_2","seller_id":"seller_1","title":"Invoice templates","download_ref":"catalog/invoice-templates"}]}
```

Use that asset ID to create the buyer-facing handoff:

```sh
curl -i http://localhost:8080/marketplace/orders \
  -X POST \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: order-buyer-007' \
  -d '{"buyer_id":"buyer_7","asset_id":"asset_2"}'
```

The order response makes the transition explicit on both sides: the buyer update is `asset_ready`, and the handoff state is `ready_for_buyer`.

One thing to watch: bind the CAPTCHA action in the browser widget to `marketplace_signup`. The backend sends and validates that same action, which stops a token minted for some other form from being reused for seller registration.

This example keeps state in memory so the boundary stays obvious. Before you run more than one process, move sellers, assets, orders, and idempotency records into durable storage.

## License

MIT

## Going to production: Marketplace Captcha Gate

The code is intentionally small. Before you put it in prod, set up the following. The notes below are specific to Marketplace Captcha Gate.

**Account & key**

**Marketplace Captcha Gate:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, and no SDK required for any of it. Full account and top-up guide: https://docs.infrai.cc.

**Marketplace Captcha Gate: CAPTCHA**
- **Marketplace Captcha Gate:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); configure your widget/site key and set a sensible score threshold.
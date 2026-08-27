# XLWMS API Manager

## API Routing

- Use `https://api.xlwms.com/openapi` as the default OpenAPI base URL.
- Keep the runtime endpoint configurable through `XLWMS_API_BASE_URL`.
- The parcel outbound-order page endpoint is `POST /v1/outboundOrder/pageList`.

## Credentials

- Store real credentials only in the local `.env` file.
- Load credentials from `XLWMS_APP_KEY` and `XLWMS_APP_SECRET`.
- Never copy credential values into source code, tests, documentation, logs, or command output.
- Keep `.env` ignored by Git and restricted to the current user with file mode `600`.

## API Safety

- Do not invent a signing algorithm. Implement authentication only from official documentation or a verified request example.
- Raw API exports may contain customer and logistics data. Write exports with file mode `600`.

## Related Temu Service

- The independent Temu Go service lives at `/home/ubuntu/temu-api-manager` and owns Temu credentials, request signing, package numbers, and calls to `temu.track.trackinginfo.get`.
- XLWMS must use the Temu Go service for tracking instead of signing or sending Temu OpenAPI requests directly.
- The local Temu service defaults to `http://127.0.0.1:18082/temu`; keep it configurable through `TEMU_GO_BASE_URL`.
- Select the Temu shop with `X-Temu-Shop`. Order tracking is `GET /api/orders/{parentOrderSN}/tracking?language=en` and package tracking is `GET /api/packages/{packageSn}/tracking?language=en`.
- XLWMS owns the cross-shop and cross-warehouse tracking monitor. Store only the normalized tracking state needed for operations; do not copy Temu shop credentials or raw tracking responses into this project.


## Warehouse Registry

- Store per-warehouse credentials encrypted in `xlwms_warehouses`.
- Keep the Fernet master key only in `.warehouse_credentials_key` with mode `600`.
- Never print decrypted app keys or secrets; warehouse lists show only an app-key hint.
- Synchronization must obtain credentials through `list_active_warehouse_credentials` so disabled warehouses are skipped.

## Production Deployment

- Do not start or leave isolated trial API or frontend services after making changes.
- After implementing and verifying requested changes, publish them directly to the production service with `make start`, which builds the Go server and frontend and reloads the PM2 process configuration.
- After every deployment, verify the production health endpoint and the affected production API route. Do not expose credentials or customer/logistics payloads while verifying.

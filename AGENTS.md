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


## Warehouse Registry

- Store per-warehouse credentials encrypted in `xlwms_warehouses`.
- Keep the Fernet master key only in `.warehouse_credentials_key` with mode `600`.
- Never print decrypted app keys or secrets; warehouse lists show only an app-key hint.
- Synchronization must obtain credentials through `list_active_warehouse_credentials` so disabled warehouses are skipped.

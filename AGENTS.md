# XLWMS API Manager

## API Routing

- Use `https://api.xlwms.com/openapi` as the default OpenAPI base URL.
- Keep the runtime endpoint configurable through `XLWMS_API_BASE_URL`.
- The parcel outbound-order page endpoint is `POST /v1/outboundOrder/pageList`.

## Credentials

- Store real credentials only in the local `.env` file.
- Load credentials from `XLWMS_APP_KEY` and `XLWMS_APP_SECRET`.
- Protect fulfillment-shop write APIs with HTTP Basic Auth loaded from `XLWMS_CONSOLE_USER` and `XLWMS_CONSOLE_PASSWORD`; keep reads credential-free.
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

- Model ARP's four physical warehouses as peers: Pennsylvania (`HYTX30`, warehouse 1),
  Los Angeles (`ARPCA01`, warehouse 8), Houston (`ARP06A`, warehouse 6), and Atlanta
  (`ARPGA`, reserved while the warehouse is not yet listed).
- Use `ARP-宾夕法尼亚`, `ARP-洛杉矶`, `ARP-休斯顿`, and `ARP-亚特兰大` as display names
  through `frontend/src/warehouseNames.ts`. Pennsylvania's exact city is not yet verified.
  Existing `ARP_EAST` and `ARP_WEST` identifiers locate Pennsylvania and Los Angeles;
  do not interpret these identifiers as parent warehouses or infer account ownership from them.
- Keep physical warehouse identity independent of optional geographic filters.
  Houston must participate in automatic selection once its inventory, OMS permissions,
  and platform warehouse mappings are ready. The approved design is documented in
  `docs/houston-atlanta-warehouse-design.md`. Houston fulfillment is implemented;
  Atlanta has metadata and carrier rules but defaults to disabled until listed.
- Restrict Houston and Atlanta to USPS, GOFO, UPS, and FEDEX. Treat physical carrier
  capability as an upper bound when resolving platform, SKU, or OMS account rules.
  Apply the bound to automatic and manual selection and verify the actual carrier
  before label purchase and shipping approval.
- Treat OpenAPI credentials as independent SKU data scopes. A credential can expose multiple warehouses,
  and one warehouse can be exposed by multiple credentials with different SKUs.
- Store OpenAPI credentials encrypted in `xlwms_api_credentials`; use
  `xlwms_api_credential_inventory` only for the warehouse and SKU scope discovered from that API.
- Bind each OpenAPI credential to one OMS shipping account through
  `xlwms_oms_account_api_credentials`; one OMS account may bind multiple OpenAPI credentials.
- Resolve shipping accounts from the API binding. Do not infer an account from warehouse
  codes or `arp`/`dps` prefixes, and do not add per-SKU account routing alongside the API binding.
- Keep the Fernet master key only in `.warehouse_credentials_key` with mode `600`.
- Never print decrypted app keys, OMS usernames, passwords, or tokens; lists show only credential hints.

## Production Deployment

- Do not start or leave isolated trial API or frontend services after making changes.
- After implementing and verifying requested changes, publish them directly to the production service with `make start`, which builds the Go server and frontend and reloads the PM2 process configuration.
- After every deployment, verify the production health endpoint and the affected production API route. Do not expose credentials or customer/logistics payloads while verifying.

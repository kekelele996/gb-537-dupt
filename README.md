# CertRollover

CertRollover is an offline PKI certificate rollover impact assessment service for platform-security teams. It inventories public trust anchors, certificate chains, and dependent services, then simulates a proposed trust-anchor rollover against a frozen dependency snapshot.

```bash
docker compose up -d --build
```

Open `http://127.0.0.1:18537`. The application is decision support only: it does not retain private keys, issue or revoke certificates, deploy changes, or connect to production key-management systems. A simulation is not a substitute for a production PKI change review or independent approval.

## Capabilities

- Import and fingerprint public trust-anchor material while rejecting private-key storage.
- Validate public certificate chains with Go `crypto/x509` and record the offline result.
- Model service-to-service dependencies, trust references, ownership, environment, and criticality.
- Freeze inputs for deterministic rollover simulation, with evidence for time-window path failures.
- Reconcile per-service migration receipts against the frozen simulation: service owners maintain their client trust set and report cutover progress, failures retry independently, and "broken but claimed migrated" services are held for an independent reviewer.
- Block `simulated -> ready` until every service receipt reconciles; backfill legacy simulations that predate receipts.
- Require an independent reviewer before a scenario can move from `executing` to `verified`.
- Preserve request IDs, actor identity, before/after snapshots, hashes, algorithm version, and timing in audit records.

## Roles and demo accounts

| Role | Account | Password | Scope |
| --- | --- | --- | --- |
| PKI administrator | `admin` | `admin123` | Full administration and verification |
| PKI operator | `operator` | `operator123` | Trust material, chains, dependencies, and simulations |
| Service owner | `owner` | `owner123` | Team-scoped dependency and scenario work |
| Security reviewer | `reviewer` | `reviewer123` | Independent scenario verification and audit access |
| Auditor | `auditor` | `auditor123` | Read-only audit access |

The seeded accounts are for local development only. Replace them and the JWT secret before any shared deployment.

## Architecture

| Layer | Technology | Responsibility |
| --- | --- | --- |
| Web client | React 18, TypeScript, Vite, Material UI, Zustand | Five operational pages and permission-aware controls |
| API | Go 1.22, Gin, validator/v10 | JWT/RBAC, request handling, validation, state transitions |
| Domain | Go services, repositories, GORM | Transactional persistence, audit records, deterministic simulation |
| Data | PostgreSQL 16 | Production Compose database |
| Edge | Nginx | SPA fallback and same-origin `/api` proxy |

The principal model is `TrustAnchor -> CertificateChain -> DependentService -> downstream service`. A `RolloverScenario` stores a frozen version of that model and evaluates critical time points in the supplied overlap window. The API is namespaced below `/api/v1`; the backend health endpoint is `/healthz`.

## Main routes

| UI route | API resources | Purpose |
| --- | --- | --- |
| `/anchors` | `/trust-anchors`, `/certificate-chains` | Inspect trust-anchor fingerprints, validity, and chain references |
| `/chains` | `/certificate-chains`, `/trust-anchors`, `/dependent-services` | Review chain structure and offline validation |
| `/dependencies` | `/dependent-services`, `/certificate-chains` | Maintain dependency edges and find cycles |
| `/rollovers` | `/rollover-scenarios`, receipts, and all core resources | Run, compare, replay simulations; reconcile per-service migration receipts; transition frozen simulations |
| `/audit` | `/audit-logs` and entity projections | Filter audit evidence by request, actor, entity, and time |

Authentication is available through `POST /api/v1/auth/login`. All write endpoints produce an audit record. Simulation requests require an `Idempotency-Key`; repeated requests with the same key return the stored result.

## State and safety rules

`CertificateState` is calculated by the backend and shared in:

- `backend/internal/constants/certificate_state.go`
- backend models, DTOs, x509 validation, services, and tests
- `frontend/src/types/enums/certificate-state.ts`, stores, badges, and pages

`ScenarioState` is shared in:

- `backend/internal/constants/scenario_state.go`
- backend DTOs, services, routers, and state-machine tests
- `frontend/src/types/enums/scenario-state.ts`, stores, state badges, and rollover page

The receipt workflow enums (`MigrationState`, `DeliveryState`, `ReviewStatus`, `ReconciliationStatus`) and the `ReconcileReceipt`/`ReadyBlocked` rules are shared in:

- `backend/internal/constants/scenario_state.go` plus `constants/receipt_reconciliation_test.go`
- the `MigrationReceipt` model and rollover DTO/repository/service/handler/router
- `frontend/src/types/enums/reconciliation-state.ts`, the rollover store/api/types, and `components/common/ReceiptReconciliationPanel.tsx`

Valid scenario transitions are `draft -> simulated -> ready -> executing -> verified`, `executing -> rollback`, and `simulated/ready -> draft`. Invalid transitions return `409`; a creator attempting to verify their own scenario receives `409 REVIEWER_SEPARATION_REQUIRED`. Authorization failures return `403`, and unauthenticated requests return `401`.

### Migration receipts and per-service reconciliation

When the root is replaced, the platform team freezes the trust anchors and chain, runs one simulation, and lists the broken paths. Each service owner then maintains that service's client trust set and reports cutover progress as a **migration receipt**; the two sides are reconciled per service:

- `outstanding` — the service has not finished moving to the new root. The rollover cannot be marked ready.
- `mismatch_pending_review` — the simulation predicts a break but the owner claims migrated. The row is held and only an independent `security_reviewer` (never the reporter) can clear or reject it.
- `delivery_failed` — the report for this one service failed. It retries in isolation and never blocks another service's receipt.
- `legacy_missing` — an older simulation had no receipts; the upgrade backfills only missing per-service placeholders and lists them separately. Existing owner receipts are never overwritten, and backfill is idempotent.
- `reconciled` — simulation and receipt agree.

The `simulated -> ready` transition is rejected with `409 INVALID_STATE_TRANSITION` while any service is outstanding, legacy-missing, delivery-failed, or held for review. Every receipt action writes an audit record (`submit`, `retry_delivery`, `independent_review`, `backfill_legacy`) with request ID, actor, and before/after snapshots.

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /api/v1/rollover-scenarios/:id/receipts` | any read role | Per-service reconciliation, counts, and ready gate |
| `POST /api/v1/rollover-scenarios/:id/receipts` | service owner (own team) / operator / admin | Submit or update one service receipt |
| `POST /api/v1/rollover-scenarios/:id/receipts/:service_id/retry` | service owner / operator / admin | Retry one failed receipt independently |
| `POST /api/v1/rollover-scenarios/:id/receipts/:service_id/review` | security reviewer / admin, not the reporter | Clear or reject a held mismatch |
| `POST /api/v1/rollover-scenarios/:id/receipt-backfill` | operator / admin | Fill missing receipts for a legacy simulation |


## Configuration and ports

Copy `.env.example` to `.env` for local configuration. `.env` is intentionally ignored by Git.

| Variable | Compose default | Meaning |
| --- | --- | --- |
| `FRONTEND_PORT` | `18537` | Nginx application port |
| `BACKEND_PORT` | `19537` | Go API port |
| `DB_PORT` | `57537` | PostgreSQL host port |
| `POSTGRES_DB` | `pki_rollover` | Database name |
| `POSTGRES_USER` | `pki` | Database user |
| `POSTGRES_PASSWORD` | local only | Database password |
| `JWT_SECRET` | local only | JWT signing secret, minimum 16 characters |

The Compose project is named `pki-certificate-rollover-impact`, has health checks for all services, and waits for each dependency to become healthy.

## Local development

```bash
go work sync
go build ./backend/...
go vet ./backend/...
go test ./backend/...

npm --prefix frontend ci
npm --prefix frontend run build
npm --prefix frontend test

cd backend && go run ./cmd/server
```

To run the self-contained SQLite manifest check:

```bash

```

The manifest starts the backend on `20537` with in-memory SQLite and waits for `/healthz`; it exits after the health check.

## Docker operations

```bash
docker compose config --quiet
docker compose up -d --build
docker compose ps
docker compose down -v --remove-orphans
```

`down -v` removes the local named PostgreSQL volume. Use it only when the local data can be discarded.

## Troubleshooting

- A failed Compose health check: inspect `docker compose logs backend frontend db` and verify the configured ports are unused.
- API requests returning `401`: authenticate again and send `Authorization: Bearer <token>`.
- A simulation returning `409`: use a new idempotency key for a different scenario, or follow the state-machine transition order.
- A verification rejection: use a different security reviewer; the creator is deliberately barred from self-verification.

## License

This project is provided for internal evaluation and local development. No production-use license is granted.

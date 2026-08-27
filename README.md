# hapto

A proximity payment system: two Android phones exchange a signed payment
authorization over BLE, a Go backend verifies and settles it, a Rust
service handles the cryptography. Personal project, built to enterprise
standard as a proof of capability.

## Architecture hapto-mobile (Expo/RN, Android)
                merchant + customer roles, real BLE
                        |
                      HTTPS
                        |
                +---------------+
                |   hapto-api   |   Go
                |               |
                |  Auth              |
                |  Payment Intent Svc|
                |  Signing Devices   |
                |  Ledger            |
                |  Idempotency       |
                |  Risk              |
                +-------+-------+
                        |
                   gRPC (mTLS)
                        |
                +---------------+
                |  hapto-crypto |   Rust
                |               |
                |  Ed25519 sign/verify   |
                |  Nonce generation      |
                |  Public key validation |
                +---------------+

Core rule: BLE proves two phones are near each other and lets them
exchange a signed payment intent. The backend is the source of truth for
money. `hapto-crypto` never touches Postgres or Redis. `hapto-api` never
sees a private key, only public keys and signatures it asks
`hapto-crypto` to verify.

## Services

| Service | Language | Purpose |
|---|---|---|
| `services/hapto-api` | Go 1.27 | Orchestration, ledger, payment intents, auth, device registry |
| `services/hapto-crypto` | Rust 1.98 | Signature verification, nonce generation, key validation, gRPC only |
| `apps/hapto-mobile` | Expo/React Native | The product. Real BLE on Android, merchant and customer roles |
| `apps/hapto-web` | Next.js | Minimal support surface, not the primary product |

## Prerequisites

- Go 1.27+
- Rust 1.98+ (via [rustup](https://rustup.rs))
- Node 22+
- `protoc` (Protocol Buffers compiler)
- `buf` (`npm install -g @bufbuild/buf`)
- `openssl` (for `scripts/gen-certs.sh`, the local mTLS certs)
- Docker (for Postgres and Redis locally)
- Android Studio + a physical Android device (BLE does not work in emulators)

## Local setup

Clone, then bring up infra:

```bash
docker compose up -d
docker compose ps   # confirm postgres and redis are healthy
```

Copy env files and fill in the generated secrets:

```bash
cp services/hapto-api/.env.example services/hapto-api/.env
cp services/hapto-crypto/.env.example services/hapto-crypto/.env
cp apps/hapto-mobile/.env.example apps/hapto-mobile/.env
```

Generate local mTLS certs (needed before either service will start — see
[mTLS certificates](#mtls-certificates) below):

```bash
./scripts/gen-certs.sh
```

### hapto-crypto

```bash
cd services/hapto-crypto
cargo run
```

Listens on `localhost:50051`. Requires mTLS certs (see below) — it refuses
to start without its server cert/key/CA configured.

### hapto-api

```bash
cd services/hapto-api
go run ./cmd/server
```

`cmd/server` applies every pending migration on startup before it accepts
requests, so a fresh database (a new volume, or CI's ephemeral Postgres
service) just works. There's no other schema source: `migrations/` is the
single source of truth.

#### Migrations

Migrations live in `services/hapto-api/migrations/` as paired
`NNNN_description.up.sql` / `.down.sql` files, run with
[golang-migrate](https://github.com/golang-migrate/migrate). Run them by
hand with the `cmd/migrate` command:

```bash
cd services/hapto-api

go run ./cmd/migrate up      # apply every pending migration
go run ./cmd/migrate down    # reverse every migration, back to empty
```

Both default to `$DATABASE_URL`, falling back to the local dev connection
string in `.env.example` if that's unset. Point at a different database
with `-database`:

```bash
go run ./cmd/migrate up -database "postgres://user:pass@host:5432/db?sslmode=disable"
```

Add a new migration by creating the next-numbered `.up.sql`/`.down.sql`
pair; the down file should reverse the up file exactly (drop what it
created, in reverse dependency order).

### mTLS certificates

The gRPC connection between `hapto-api` and `hapto-crypto` is mutual TLS:
each side presents a certificate the other verifies against a shared local
CA, and each refuses the connection if the peer doesn't present one signed
by it. There is no plaintext fallback.

Generate a CA and both services' certs with:

```bash
./scripts/gen-certs.sh
```

This wipes and regenerates everything under `certs/` (gitignored — nothing
here is ever committed) as:

| File | Used by |
|---|---|
| `certs/ca.crt` / `ca.key` | Trust root both services verify the other against |
| `certs/hapto-crypto.crt` / `.key` | hapto-crypto's server identity |
| `certs/hapto-api.crt` / `.key` | hapto-api's client identity |

Both services read the same three env var names — `HAPTO_CRYPTO_TLS_CERT`,
`HAPTO_CRYPTO_TLS_KEY`, `HAPTO_CRYPTO_TLS_CA` — each pointed at its own
cert/key and the shared CA (see each service's `.env.example`). Defaults
assume the standard local-dev run location (`services/hapto-crypto` and
`services/hapto-api` respectively) and the default `certs/` output above,
so a fresh `./scripts/gen-certs.sh` followed by `cargo run` /
`go run ./cmd/server` just works without setting anything.

Re-run `./scripts/gen-certs.sh` any time — it's a repeatable build step,
not a one-time secret. A real deployment generates its own certs (or uses
a real CA) and injects them however that environment manages secrets.

### hapto-mobile

```bash
cd apps/hapto-mobile
npm install
npm run android
```

Set `API_BASE_URL` in `.env` to your machine's LAN IP, not `localhost`,
since the phone resolves `localhost` to itself.

## Testing

```bash
# hapto-api
cd services/hapto-api && go vet ./... && golangci-lint run && go test -race ./...

# hapto-crypto
cd services/hapto-crypto && cargo clippy --all-targets -- -D warnings && cargo test

# hapto-mobile
cd apps/hapto-mobile && npm run lint
```

CI runs all three on every pull request, scoped to whichever service
changed.

## Working on this repo

- Branch per change: `feat/<scope>-<description>`, `fix/<scope>-<description>`, `chore/<scope>-<description>`
- Conventional commits, one logical unit per commit
- `proto/` is the single source of truth for the contract between
  `hapto-api` and `hapto-crypto`. Edit the `.proto` file, regenerate,
  don't hand edit generated code.
- See `CLAUDE.md` for the full set of architectural invariants and
  workflow rules followed in this repo, including by Claude Code.

## Status

Early build. See `CLAUDE.md` and commit history for current state.
Not production ready, no real money moves through this yet.

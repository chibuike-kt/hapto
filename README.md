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
                      gRPC
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

### hapto-crypto

```bash
cd services/hapto-crypto
cargo run
```

Listens on `localhost:50051`.

### hapto-api

```bash
cd services/hapto-api
go run ./cmd/server
```

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

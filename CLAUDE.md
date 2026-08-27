# hapto — instructions for Claude Code

## Hard rules — never violate these

- **Never run any git operation.** No `git commit`, `git push`, `git add`,
  `git checkout`, `git branch`, `git merge`, no staging, no committing,
  nothing. Kingsley reviews and commits everything himself. If a task
  seems to require a commit, stop and hand back control instead.
- **No unnecessary comments.** Comment only where the *why* isn't obvious
  from the code itself (a non-obvious constraint, a spec section being
  satisfied, a deliberate deviation from the "obvious" approach). Never
  narrate what a line does when the code already says it. No comment
  headers, no restating function names in prose above them.
- **No AI-sounding boilerplate.** No "Certainly!", no filler docstrings,
  no padding a function with a comment just to look thorough.
- Follow existing package/module boundaries, don't collapse them for
  convenience:
  - `hapto-api/internal/`: one concern per package (intent, ledger,
    device, idempotency, risk)
  - `hapto-crypto/src/`: one module per concern (verify, nonce, keys)
  - `hapto-mobile/src/`: merchant and customer roles stay separated,
    don't share state across them beyond what the BLE session actually
    exchanges

## Project

hapto: a proximity payment system, BLE handshake between two phones,
backed by a Go API and a Rust crypto service. Personal project, built
to enterprise standard as a proof of capability, not a startup feature.
Stack: Go 1.26.4, Rust 1.98.0, Next.js 16.3.3, Expo SDK 57 (RN 0.86),
Postgres 18.4, Redis 8.10.0, Docker.

## Services

- `services/hapto-api`    Go — orchestration, ledger, payment intents,
  device registry, idempotency, risk
- `services/hapto-crypto` Rust — Ed25519 signature verification, nonce
  generation, public key validation. gRPC only, nothing else touches it.
- `apps/hapto-web`        Next.js — merchant dashboard, control plane
- `apps/hapto-mobile`     Expo/React Native — real BLE on Android,
  merchant peripheral role via a native Kotlin module, customer central
  role via react-native-ble-plx

## Core architectural rule

BLE proves two phones are near each other and lets them exchange a
signed payment intent. The backend is the source of truth for money.
`hapto-crypto` never touches Postgres or Redis. `hapto-api` never sees
a private key, only public keys and signatures it asks `hapto-crypto`
to verify. `proto/` is the single source of truth for the contract
between them, nothing hand-written duplicates it.

## Non-negotiable invariants

- All money is integer minor units, never float, in any language, anywhere.
- Every payment intent has a server-generated `payment_intent_id`, the
  client never invents one, and every write against it is idempotent.
- Every signed payload carries a nonce and is checked against the
  session it claims to belong to, replay of a valid signature is a
  rejection, not a warning.
- `ledger_entries` is insert-only. Never generate an UPDATE or DELETE
  against it, corrections are new entries, not edits.
- Signature verification only happens inside `hapto-crypto`, never
  reimplemented in Go, TypeScript, or Kotlin, even for a quick check.
- A revoked device fails verification even with a valid signature,
  check device status before trusting a signature result.
- Payment intent state transitions follow the lifecycle CREATED →
  PENDING → CUSTOMER_AUTHORIZED → PROCESSING → COMPLETED (with EXPIRED,
  FAILED, REVERSED as failure branches). The backend owns transitions,
  never the client.

## Workflow

- Work in small, reviewable vertical slices, finish one end to end
  (e.g. "device registration end to end with tests") before starting
  the next.
- Before considering anything done, run per service:
  - `hapto-api`: `go vet ./... && golangci-lint run && go test -race ./...`
  - `hapto-crypto`: `cargo clippy --all-targets -- -D warnings && cargo test`
  - `hapto-web` / `hapto-mobile`: `npm run lint && npm run build`
- Leave the working tree dirty for Kingsley to review and commit, do
  not stage or commit it yourself, per the hard rule above.

package intent_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	haptov1 "github.com/chibuike-kt/hapto-api/gen/hapto/v1"
	"github.com/chibuike-kt/hapto-api/internal/audit"
	"github.com/chibuike-kt/hapto-api/internal/cryptoclient"
	"github.com/chibuike-kt/hapto-api/internal/device"
	"github.com/chibuike-kt/hapto-api/internal/intent"
	"github.com/chibuike-kt/hapto-api/internal/ledger"
)

// testCertsDir mirrors internal/cryptoclient's test helper: certs live at
// the repo root, reached relative to this package's directory.
const testCertsDir = "../../../../certs"

func dialTestCryptoClient(t *testing.T) *cryptoclient.Client {
	t.Helper()

	addr := os.Getenv("HAPTO_CRYPTO_ADDR")
	if addr == "" {
		addr = "localhost:50051"
	}

	tlsCfg := cryptoclient.TLSConfig{
		CertFile: envOr("HAPTO_CRYPTO_TLS_CERT", testCertsDir+"/hapto-api.crt"),
		KeyFile:  envOr("HAPTO_CRYPTO_TLS_KEY", testCertsDir+"/hapto-api.key"),
		CAFile:   envOr("HAPTO_CRYPTO_TLS_CA", testCertsDir+"/ca.crt"),
	}

	client, err := cryptoclient.Dial(addr, tlsCfg)
	if err != nil {
		t.Skipf("hapto-crypto not available: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Logf("close crypto client: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := client.ValidatePublicKey(ctx, make([]byte, 32), haptov1.SignatureAlgorithm_SIGNATURE_ALGORITHM_ED25519); err != nil {
		t.Skipf("hapto-crypto not reachable at %s: %v", addr, err)
	}

	return client
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// testEnv wires every real dependency this phase ties together: real
// Postgres-backed stores for device/ledger/audit/intent, and a real
// cryptoclient.Client talking to a real hapto-crypto over mTLS. Nothing
// here is a fake — this is exactly the "end-to-end verification matters
// more here than in any prior PR" ask.
type testEnv struct {
	pool          *pgxpool.Pool
	crypto        *cryptoclient.Client
	deviceService *device.Service
	ledgerService *ledger.Service
	auditStore    *audit.PostgresStore
	intentService *intent.Service

	userIDs   []string
	deviceIDs []string
	walletIDs []string
	txIDs     []string
	intentIDs []string
}

func newTestEnv(t *testing.T, ttl time.Duration) *testEnv {
	t.Helper()
	pool := openTestPool(t)
	crypto := dialTestCryptoClient(t)

	auditStore := audit.NewPostgresStore(pool)
	deviceService := device.NewService(device.NewPostgresStore(pool), crypto, auditStore)
	ledgerService := ledger.NewService(ledger.NewPostgresStore(pool))
	intentService := intent.NewService(
		intent.NewPostgresStore(pool), ledgerService, deviceService, crypto, auditStore, ttl,
	)

	env := &testEnv{
		pool: pool, crypto: crypto,
		deviceService: deviceService, ledgerService: ledgerService,
		auditStore: auditStore, intentService: intentService,
	}

	t.Cleanup(func() {
		ctx := context.Background()
		exec := func(query string, args ...any) {
			if _, err := pool.Exec(ctx, query, args...); err != nil {
				t.Logf("cleanup: %q: %v", query, err)
			}
		}
		if len(env.txIDs) > 0 {
			exec("DELETE FROM ledger_entries WHERE transaction_id = ANY($1)", env.txIDs)
			exec("DELETE FROM ledger_transactions WHERE id = ANY($1)", env.txIDs)
		}
		if len(env.intentIDs) > 0 {
			exec("DELETE FROM payment_authorizations WHERE payment_intent_id = ANY($1)", env.intentIDs)
			exec("DELETE FROM audit_logs WHERE target_id = ANY($1)", env.intentIDs)
			exec("DELETE FROM payment_intents WHERE id = ANY($1)", env.intentIDs)
		}
		if len(env.walletIDs) > 0 {
			exec("DELETE FROM wallets WHERE id = ANY($1)", env.walletIDs)
		}
		if len(env.deviceIDs) > 0 {
			exec("DELETE FROM audit_logs WHERE target_id = ANY($1)", env.deviceIDs)
			exec("DELETE FROM signing_devices WHERE id = ANY($1)", env.deviceIDs)
		}
		if len(env.userIDs) > 0 {
			exec("DELETE FROM audit_logs WHERE actor_user_id = ANY($1)", env.userIDs)
			exec("DELETE FROM users WHERE id = ANY($1)", env.userIDs)
		}
	})

	return env
}

// newUser inserts a minimal users row — needed because audit_logs.actor_user_id
// has a foreign key to users(id): without a real row, audit writes for this
// actor would silently fail (by design — see internal/auth/device's
// logAudit) and the audit assertions below would find nothing.
func (e *testEnv) newUser(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password_hash, status, created_at)
		VALUES ($1, $2, 'unused', 'active', now())
	`, id, id+"@example.test")
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	e.userIDs = append(e.userIDs, id)
	return id
}

func (e *testEnv) newWallet(t *testing.T, userID string) *ledger.Wallet {
	t.Helper()
	w, err := e.ledgerService.CreateWallet(context.Background(), userID, "USD")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	e.walletIDs = append(e.walletIDs, w.ID)
	return w
}

// newRegisteredDevice generates a real Ed25519 key pair and registers it
// through the real device.Service, which itself calls the real
// hapto-crypto ValidatePublicKey RPC.
func (e *testEnv) newRegisteredDevice(t *testing.T, userID string) (*device.Device, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	d, err := e.deviceService.Register(context.Background(), device.RegisterInput{
		UserID:    userID,
		PublicKey: pub,
		Algorithm: device.AlgorithmEd25519,
	})
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	e.deviceIDs = append(e.deviceIDs, d.ID)
	return d, priv
}

func (e *testEnv) createIntent(t *testing.T, merchantUserID string, amount int64) *intent.Intent {
	t.Helper()
	in, err := e.intentService.Create(context.Background(), intent.CreateInput{
		MerchantUserID: merchantUserID,
		Amount:         amount,
		Currency:       "USD",
		IdempotencyKey: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create intent: %v", err)
	}
	e.intentIDs = append(e.intentIDs, in.ID)
	return in
}

// signPayload builds a payload that incorporates the intent's nonce (as
// required by Authorize) plus a little context, and signs it with the
// customer's real private key — exactly what the mobile app's customer
// role would do after receiving the nonce over BLE.
func signPayload(t *testing.T, priv ed25519.PrivateKey, in *intent.Intent) (payload, signature []byte) {
	t.Helper()
	type signedPayload struct {
		Nonce           string `json:"nonce"`
		PaymentIntentID string `json:"payment_intent_id"`
		Amount          int64  `json:"amount"`
	}
	// The nonce must be a byte-prefix of the payload (Service.Authorize's
	// binding check), so encode it as a raw prefix, then append context.
	suffix, err := json.Marshal(signedPayload{
		Nonce:           base64.StdEncoding.EncodeToString(in.Nonce),
		PaymentIntentID: in.ID,
		Amount:          in.Amount,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	payload = append(append([]byte{}, in.Nonce...), suffix...)
	signature = ed25519.Sign(priv, payload)
	return payload, signature
}

func TestIntegration_FullFlow_CreateAuthorizeSettleCompletes(t *testing.T) {
	env := newTestEnv(t, 5*time.Minute)

	merchantUserID := env.newUser(t)
	customerUserID := env.newUser(t)
	env.newWallet(t, merchantUserID)
	env.newWallet(t, customerUserID)

	dev, priv := env.newRegisteredDevice(t, customerUserID)

	const amount = int64(1234)
	in := env.createIntent(t, merchantUserID, amount)
	if in.Status != intent.StatusPending {
		t.Fatalf("status after create = %s, want %s", in.Status, intent.StatusPending)
	}
	if len(in.Nonce) == 0 {
		t.Fatal("expected a real nonce from hapto-crypto")
	}

	payload, signature := signPayload(t, priv, in)

	final, err := env.intentService.Authorize(context.Background(), in.ID, intent.AuthorizeInput{
		CustomerSigningDeviceID: dev.ID,
		Signature:               signature,
		SignedPayload:           payload,
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if final.Status != intent.StatusCompleted {
		t.Fatalf("final status = %s, want %s", final.Status, intent.StatusCompleted)
	}

	// The ledger must actually show balanced entries: customer debited,
	// merchant credited, by exactly the intent amount.
	customerWallet, err := env.ledgerService.GetWalletByUserID(context.Background(), customerUserID, "USD")
	if err != nil {
		t.Fatalf("get customer wallet: %v", err)
	}
	merchantWallet, err := env.ledgerService.GetWalletByUserID(context.Background(), merchantUserID, "USD")
	if err != nil {
		t.Fatalf("get merchant wallet: %v", err)
	}

	custBal, err := env.ledgerService.Balance(context.Background(), customerWallet.ID)
	if err != nil {
		t.Fatalf("customer balance: %v", err)
	}
	if custBal != -amount {
		t.Fatalf("customer balance = %d, want %d", custBal, -amount)
	}
	merchBal, err := env.ledgerService.Balance(context.Background(), merchantWallet.ID)
	if err != nil {
		t.Fatalf("merchant balance: %v", err)
	}
	if merchBal != amount {
		t.Fatalf("merchant balance = %d, want %d", merchBal, amount)
	}

	// Track the transaction for cleanup: find it by the intent's ID, which
	// is the idempotency key settle() uses.
	var txID string
	if err := env.pool.QueryRow(context.Background(),
		"SELECT id FROM ledger_transactions WHERE idempotency_key = $1", in.ID,
	).Scan(&txID); err != nil {
		t.Fatalf("find settlement transaction: %v", err)
	}
	env.txIDs = append(env.txIDs, txID)

	assertAuditEntryExists(t, env.pool, audit.ActionDeviceRegistered, dev.ID)
	assertAuditEntryExists(t, env.pool, audit.ActionPaymentIntentCreated, in.ID)
	assertAuditEntryExists(t, env.pool, audit.ActionPaymentIntentAuthorized, in.ID)
	assertAuditEntryExists(t, env.pool, audit.ActionPaymentIntentCompleted, in.ID)
}

func assertAuditEntryExists(t *testing.T, pool *pgxpool.Pool, action, targetID string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM audit_logs WHERE action = $1 AND target_id = $2", action, targetID,
	).Scan(&count); err != nil {
		t.Fatalf("count audit entries for %s: %v", action, err)
	}
	if count == 0 {
		t.Fatalf("expected at least one audit_logs row for action %q targeting %s", action, targetID)
	}
}

func TestIntegration_AuthorizeWithRevokedDevice_Rejected(t *testing.T) {
	env := newTestEnv(t, 5*time.Minute)

	merchantUserID := env.newUser(t)
	customerUserID := env.newUser(t)
	env.newWallet(t, merchantUserID)
	env.newWallet(t, customerUserID)

	dev, priv := env.newRegisteredDevice(t, customerUserID)
	if err := env.deviceService.Revoke(context.Background(), dev.ID, customerUserID); err != nil {
		t.Fatalf("revoke device: %v", err)
	}

	in := env.createIntent(t, merchantUserID, 500)
	payload, signature := signPayload(t, priv, in)

	_, err := env.intentService.Authorize(context.Background(), in.ID, intent.AuthorizeInput{
		CustomerSigningDeviceID: dev.ID,
		Signature:               signature,
		SignedPayload:           payload,
	})
	if !errors.Is(err, device.ErrDeviceRevoked) {
		t.Fatalf("expected device.ErrDeviceRevoked, got %v", err)
	}

	got, err := env.intentService.GetByID(context.Background(), in.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Status != intent.StatusPending {
		t.Fatalf("status = %s, want %s (must be unchanged)", got.Status, intent.StatusPending)
	}
}

// TestIntegration_ReplayedValidSignature_Rejected proves a mathematically
// valid signature isn't sufficient on its own once it's already produced
// an authorization — the second Authorize call uses the exact same
// signature and payload that just succeeded.
func TestIntegration_ReplayedValidSignature_Rejected(t *testing.T) {
	env := newTestEnv(t, 5*time.Minute)

	merchantUserID := env.newUser(t)
	customerUserID := env.newUser(t)
	env.newWallet(t, merchantUserID)
	env.newWallet(t, customerUserID)

	dev, priv := env.newRegisteredDevice(t, customerUserID)
	in := env.createIntent(t, merchantUserID, 250)
	payload, signature := signPayload(t, priv, in)

	authInput := intent.AuthorizeInput{
		CustomerSigningDeviceID: dev.ID,
		Signature:               signature,
		SignedPayload:           payload,
	}

	first, err := env.intentService.Authorize(context.Background(), in.ID, authInput)
	if err != nil {
		t.Fatalf("first authorize: %v", err)
	}
	if first.Status == intent.StatusCompleted {
		var txID string
		if err := env.pool.QueryRow(context.Background(),
			"SELECT id FROM ledger_transactions WHERE idempotency_key = $1", in.ID,
		).Scan(&txID); err == nil {
			env.txIDs = append(env.txIDs, txID)
		}
	}

	_, err = env.intentService.Authorize(context.Background(), in.ID, authInput)
	if !errors.Is(err, intent.ErrAuthorizationReplayed) {
		t.Fatalf("expected ErrAuthorizationReplayed on replay, got %v", err)
	}
}

func TestIntegration_SettlementFailure_MovesToFailed(t *testing.T) {
	env := newTestEnv(t, 5*time.Minute)

	merchantUserID := env.newUser(t)
	customerUserID := env.newUser(t)
	env.newWallet(t, customerUserID)
	// Deliberately no merchant wallet: settlement's GetWalletByUserID for
	// the merchant will fail, forcing settle() down its failure path.

	dev, priv := env.newRegisteredDevice(t, customerUserID)
	in := env.createIntent(t, merchantUserID, 500)
	payload, signature := signPayload(t, priv, in)

	final, err := env.intentService.Authorize(context.Background(), in.ID, intent.AuthorizeInput{
		CustomerSigningDeviceID: dev.ID,
		Signature:               signature,
		SignedPayload:           payload,
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if final.Status != intent.StatusFailed {
		t.Fatalf("status = %s, want %s (must never be left in PROCESSING)", final.Status, intent.StatusFailed)
	}

	assertAuditEntryExists(t, env.pool, audit.ActionPaymentIntentFailed, in.ID)
}

package cryptoclient_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"testing"
	"time"

	haptov1 "github.com/chibuike-kt/hapto-api/gen/hapto/v1"
	"github.com/chibuike-kt/hapto-api/internal/cryptoclient"
)

// dialTestClient connects to a real hapto-crypto instance. gRPC connects
// lazily, so Dial alone can't tell us whether the server is reachable; we
// ping it with a real call and skip the test if that fails.
func dialTestClient(t *testing.T) *cryptoclient.Client {
	t.Helper()

	addr := os.Getenv("HAPTO_CRYPTO_ADDR")
	if addr == "" {
		addr = "localhost:50051"
	}

	client, err := cryptoclient.Dial(addr)
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

func TestValidatePublicKey_RejectsMalformedKey(t *testing.T) {
	client := dialTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	valid, reason, err := client.ValidatePublicKey(ctx, []byte("too-short"), haptov1.SignatureAlgorithm_SIGNATURE_ALGORITHM_ED25519)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Fatal("expected malformed key to be rejected")
	}
	if reason == "" {
		t.Error("expected a reason for rejection")
	}
}

func TestValidatePublicKey_AcceptsValidKey(t *testing.T) {
	client := dialTestClient(t)

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	valid, reason, err := client.ValidatePublicKey(ctx, pub, haptov1.SignatureAlgorithm_SIGNATURE_ALGORITHM_ED25519)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Fatalf("expected valid ed25519 key to be accepted, got reason: %s", reason)
	}
}

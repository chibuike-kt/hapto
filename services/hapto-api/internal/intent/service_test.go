package intent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chibuike-kt/hapto-api/internal/intent"
)

// fakeStore fails any call the pure-validation tests shouldn't reach —
// they exist to prove Create's input validation short-circuits before ever
// touching the store.
type fakeStore struct {
	t *testing.T
}

func (f *fakeStore) Create(context.Context, *intent.Intent) (*intent.Intent, bool, error) {
	f.t.Fatal("store.Create should not be called when input validation fails")
	return nil, false, nil
}
func (f *fakeStore) GetByID(context.Context, string) (*intent.Intent, error) {
	f.t.Fatal("store.GetByID should not be called")
	return nil, nil
}
func (f *fakeStore) TransitionToPending(context.Context, string, []byte, time.Time) error {
	f.t.Fatal("store.TransitionToPending should not be called")
	return nil
}
func (f *fakeStore) RecordAuthorization(context.Context, string, *intent.Authorization) error {
	f.t.Fatal("store.RecordAuthorization should not be called")
	return nil
}
func (f *fakeStore) GetAuthorizationByIntentID(context.Context, string) (*intent.Authorization, error) {
	f.t.Fatal("store.GetAuthorizationByIntentID should not be called")
	return nil, nil
}
func (f *fakeStore) TransitionToProcessing(context.Context, string) error {
	f.t.Fatal("store.TransitionToProcessing should not be called")
	return nil
}
func (f *fakeStore) TransitionToCompleted(context.Context, string) error {
	f.t.Fatal("store.TransitionToCompleted should not be called")
	return nil
}
func (f *fakeStore) TransitionToFailed(context.Context, string) error {
	f.t.Fatal("store.TransitionToFailed should not be called")
	return nil
}
func (f *fakeStore) SweepExpired(context.Context) ([]intent.ExpiredIntent, error) {
	f.t.Fatal("store.SweepExpired should not be called")
	return nil, nil
}

func newValidationOnlyService(t *testing.T) *intent.Service {
	return intent.NewService(&fakeStore{t: t}, nil, nil, nil, nil, 5*time.Minute)
}

func TestService_Create_RejectsNonPositiveAmount(t *testing.T) {
	svc := newValidationOnlyService(t)

	_, err := svc.Create(context.Background(), intent.CreateInput{
		MerchantUserID: "merchant-1",
		Amount:         0,
		Currency:       "USD",
		IdempotencyKey: "key-1",
	})
	if !errors.Is(err, intent.ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestService_Create_RejectsMissingCurrency(t *testing.T) {
	svc := newValidationOnlyService(t)

	_, err := svc.Create(context.Background(), intent.CreateInput{
		MerchantUserID: "merchant-1",
		Amount:         500,
		Currency:       "",
		IdempotencyKey: "key-1",
	})
	if !errors.Is(err, intent.ErrInvalidCurrency) {
		t.Fatalf("expected ErrInvalidCurrency, got %v", err)
	}
}

func TestService_Create_RejectsMissingIdempotencyKey(t *testing.T) {
	svc := newValidationOnlyService(t)

	_, err := svc.Create(context.Background(), intent.CreateInput{
		MerchantUserID: "merchant-1",
		Amount:         500,
		Currency:       "USD",
		IdempotencyKey: "",
	})
	if !errors.Is(err, intent.ErrMissingIdempotencyKey) {
		t.Fatalf("expected ErrMissingIdempotencyKey, got %v", err)
	}
}

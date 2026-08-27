package intent

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/chibuike-kt/hapto-api/internal/device"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type createRequest struct {
	MerchantUserID string `json:"merchant_user_id"`
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency"`
}

func intentResponse(in *Intent) map[string]any {
	resp := map[string]any{
		"id":               in.ID,
		"merchant_user_id": in.MerchantUserID,
		"amount":           in.Amount,
		"currency":         in.Currency,
		"status":           in.Status,
		"created_at":       in.CreatedAt.Format(time.RFC3339),
		"updated_at":       in.UpdatedAt.Format(time.RFC3339),
	}
	if in.ExpiresAt != nil {
		resp["expires_at"] = in.ExpiresAt.Format(time.RFC3339)
	}
	if len(in.Nonce) > 0 {
		resp["nonce"] = base64.StdEncoding.EncodeToString(in.Nonce)
	}
	return resp
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	got, err := h.service.Create(r.Context(), CreateInput{
		MerchantUserID: req.MerchantUserID,
		Amount:         req.Amount,
		Currency:       req.Currency,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrIdempotencyConflict):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrInvalidAmount), errors.Is(err, ErrInvalidCurrency), errors.Is(err, ErrMissingIdempotencyKey):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to create payment intent")
		}
		return
	}

	writeJSON(w, http.StatusCreated, intentResponse(got))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	got, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "payment intent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get payment intent")
		return
	}

	writeJSON(w, http.StatusOK, intentResponse(got))
}

type authorizeRequest struct {
	CustomerSigningDeviceID string `json:"customer_signing_device_id"`
	Signature               string `json:"signature"`      // base64
	SignedPayload           string `json:"signed_payload"` // base64
}

func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req authorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	signature, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		writeError(w, http.StatusBadRequest, "signature must be base64-encoded")
		return
	}
	payload, err := base64.StdEncoding.DecodeString(req.SignedPayload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "signed_payload must be base64-encoded")
		return
	}

	got, err := h.service.Authorize(r.Context(), id, AuthorizeInput{
		CustomerSigningDeviceID: req.CustomerSigningDeviceID,
		Signature:               signature,
		SignedPayload:           payload,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, device.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, device.ErrDeviceRevoked):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, ErrInvalidSignature), errors.Is(err, ErrNonceMismatch):
			writeError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrAuthorizationReplayed), errors.Is(err, ErrInvalidTransition):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to authorize payment intent")
		}
		return
	}

	writeJSON(w, http.StatusOK, intentResponse(got))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

package device

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type registerRequest struct {
	UserID    string `json:"user_id"`
	PublicKey string `json:"public_key"` // base64-encoded raw key bytes
	Algorithm string `json:"algorithm"`
}

type registerResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Algorithm string `json:"algorithm"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	publicKey, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "public_key must be base64-encoded")
		return
	}

	d, err := h.service.Register(r.Context(), RegisterInput{
		UserID:    req.UserID,
		PublicKey: publicKey,
		Algorithm: Algorithm(req.Algorithm),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidPublicKey), errors.Is(err, ErrUnsupportedAlgorithm):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to register device")
		}
		return
	}

	writeJSON(w, http.StatusCreated, registerResponse{
		ID:        d.ID,
		UserID:    d.UserID,
		Algorithm: string(d.Algorithm),
		Status:    string(d.Status),
		CreatedAt: d.CreatedAt.Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

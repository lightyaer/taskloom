package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lightyaer/taskloom/pkg/auth"
)

// AuthHandler handles authentication requests
type AuthHandler struct {
	authService *auth.AuthService
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(authService *auth.AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

// HandleEmailLogin handles email login requests
func (h *AuthHandler) HandleEmailLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" {
		respondError(w, http.StatusBadRequest, "Email is required")
		return
	}

	ctx := r.Context()
	if err := h.authService.LoginWithEmail(ctx, req.Email); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Login link sent to email"})
}

// HandleEmailVerify handles email verification requests
func (h *AuthHandler) HandleEmailVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, "Token is required")
		return
	}

	ctx := r.Context()
	jwtToken, err := h.authService.VerifyEmailToken(ctx, token)
	if err != nil {
		respondError(w, http.StatusUnauthorized, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"token": jwtToken})
}

// HandleOAuthLogin handles OAuth login requests
func (h *AuthHandler) HandleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider   string `json:"provider"`
		ProviderID string `json:"provider_id"`
		Email      string `json:"email"`
		Name       string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Provider == "" || req.ProviderID == "" || req.Email == "" || req.Name == "" {
		respondError(w, http.StatusBadRequest, "Provider, provider_id, email, and name are required")
		return
	}

	ctx := r.Context()
	jwtToken, err := h.authService.LoginWithOAuth(ctx, req.Provider, req.ProviderID, req.Email, req.Name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"token": jwtToken})
}

// SetupAuthRoutes sets up authentication routes
func (h *AuthHandler) SetupAuthRoutes(r chi.Router) {
	r.Post("/auth/email/login", h.HandleEmailLogin)
	r.Get("/auth/email/verify", h.HandleEmailVerify)
	r.Post("/auth/oauth/login", h.HandleOAuthLogin)
} 
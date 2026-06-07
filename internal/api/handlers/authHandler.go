package handlers

import (
	"RadioPump/internal/api/services"
	"errors"
	"net/http"
	"time"

	"encoding/json"
)

type AuthHandler struct {
	authService *services.AuthService
}

type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	TokenType string    `json:"token_type"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewAuthHandler(authService *services.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

// Login принимает учетные данные администратора и возвращает Bearer JWT.
// Клиент должен отправлять его в заголовке Authorization для админских методов.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var payload loginRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный json")
		return
	}

	token, expiresAt, err := h.authService.Login(payload.Name, payload.Password)
	if errors.Is(err, services.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "неверный логин или пароль")
		return
	}
	if errors.Is(err, services.ErrInvalidJWTSecret) {
		writeError(w, http.StatusInternalServerError, "jwt secret не настроен")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось создать токен")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresAt: expiresAt,
	})
}

// Me нужен фронтенду для быстрой проверки, что токен еще жив.
func (h *AuthHandler) Me(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"role": "admin"})
}

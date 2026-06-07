package middleware

import (
	"RadioPump/internal/api/services"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type MiddlewareAdmin interface {
	AdminOnly(next http.Handler) http.Handler
}

type middlewareAdmin struct {
	jwtSecret []byte
}

// NewMiddlewareAdmin создает middleware для маршрутов, где есть работа с
// файлами или админские операции. Публичные ручки должны подключаться отдельно.
func NewMiddlewareAdmin(jwtSecret string) MiddlewareAdmin {
	return &middlewareAdmin{jwtSecret: []byte(jwtSecret)}
}

func (m *middlewareAdmin) AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString := bearerToken(r.Header.Get("Authorization"))
		if tokenString == "" {
			writeAuthError(w, http.StatusUnauthorized, "требуется авторизация")
			return
		}

		claims := &services.AdminClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, jwt.ErrSignatureInvalid
			}
			return m.jwtSecret, nil
		})
		if err != nil || token == nil || !token.Valid {
			writeAuthError(w, http.StatusUnauthorized, "токен недействителен")
			return
		}
		if claims.Role != "admin" {
			writeAuthError(w, http.StatusForbidden, "недостаточно прав")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

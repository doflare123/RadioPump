package services

import (
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const adminTokenTTL = 24 * time.Hour

var (
	ErrInvalidCredentials = errors.New("неверный логин или пароль")
	ErrInvalidJWTSecret   = errors.New("jwt secret не настроен")
)

// AuthService проверяет учетные данные администратора и выпускает JWT.
// Сейчас админ один и берется из config.yaml; позже этот слой можно заменить
// на пользователей из БД, LDAP или другой источник без изменения HTTP handler.
type AuthService struct {
	adminName     string
	adminPassword string
	jwtSecret     []byte
}

// AdminClaims описывает полезную нагрузку админского JWT.
// RegisteredClaims дает стандартные exp/iat/sub для проверки токена.
type AdminClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func NewAuthService(adminName, adminPassword, jwtSecret string) *AuthService {
	return &AuthService{
		adminName:     strings.TrimSpace(adminName),
		adminPassword: adminPassword,
		jwtSecret:     []byte(jwtSecret),
	}
}

// Login проверяет логин и пароль постоянным сравнением строк одинаковой длины.
// Это дешево и убирает очевидную timing-разницу для конфигового пароля.
func (s *AuthService) Login(name, password string) (string, time.Time, error) {
	if len(s.jwtSecret) == 0 {
		return "", time.Time{}, ErrInvalidJWTSecret
	}
	if !constantTimeEqual(strings.TrimSpace(name), s.adminName) || !constantTimeEqual(password, s.adminPassword) {
		return "", time.Time{}, ErrInvalidCredentials
	}

	now := time.Now().UTC()
	expiresAt := now.Add(adminTokenTTL)
	claims := AdminClaims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   s.adminName,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", time.Time{}, err
	}

	return signed, expiresAt, nil
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

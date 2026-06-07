package services

import (
	"errors"
	"testing"
)

func TestAuthServiceLoginCreatesToken(t *testing.T) {
	service := NewAuthService("Admin", "secret", "jwt-secret")

	token, expiresAt, err := service.Login("Admin", "secret")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token == "" {
		t.Fatal("Login() returned empty token")
	}
	if expiresAt.IsZero() {
		t.Fatal("Login() returned zero expiration")
	}
}

func TestAuthServiceLoginRejectsInvalidPassword(t *testing.T) {
	service := NewAuthService("Admin", "secret", "jwt-secret")

	_, _, err := service.Login("Admin", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

package main

import (
	"os"
	"testing"
)

// Запуск должен завершиться ошибкой до создания БД или каталога музыки.
func TestStartupRejectsEmptyJWTSecret(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.yaml", []byte("server:\n  admin_name: admin\n  admin_password: long-password\n  jwt_secret: ''\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewServer(); err == nil {
		t.Fatal("startup accepted empty JWT secret")
	}
	if _, err := os.Stat("data"); !os.IsNotExist(err) {
		t.Fatalf("startup touched data directory: %v", err)
	}
}

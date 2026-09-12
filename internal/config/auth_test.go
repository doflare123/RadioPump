package config

import (
	"os"
	"strings"
	"testing"
)

// Небезопасные настройки должны прерывать загрузку конфига до создания ресурсов
// сервера. Пустой конфиг проверяет случай отсутствия обязательных полей YAML.
func TestConfigRejectsUnsafeAuth(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, body := range []string{
		"{}",
		"server:\n  jwt_secret: ''",
		"server:\n  jwt_secret: short",
		"server:\n  jwt_secret: '                                '",
	} {
		if err := os.WriteFile("config.yaml", []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewConfig(); err == nil {
			t.Fatal("accepted unsafe configuration")
		}
	}
}

// Проверяем поля авторизации отдельно, чтобы обнаруживать ошибку каждого поля.
func TestValidateAuth(t *testing.T) {
	valid := ServerConfig{AdminName: "admin", AdminPassword: "a-long-password", JWTSecret: "0123456789abcdef0123456789abcdef"}
	if err := valid.ValidateAuth(); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"admin_name", "admin_password", "jwt_secret"} {
		cfg := valid
		switch field {
		case "admin_name":
			cfg.AdminName = "  "
		case "admin_password":
			cfg.AdminPassword = "short"
		case "jwt_secret":
			cfg.JWTSecret = "short"
		}
		if err := cfg.ValidateAuth(); err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("expected error for %s: %v", field, err)
		}
	}
}

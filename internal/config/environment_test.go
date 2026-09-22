package config

import (
	"os"
	"strings"
	"testing"
)

// Окружение перекрывает YAML, включая явно пустые значения; ошибки не раскрывают секрет.
func TestEnvironmentAndSecretFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.yaml", []byte("server:\n  admin_name: admin\n  admin_password: yaml-password-123\n  jwt_secret: 0123456789abcdef0123456789abcdef\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RADIOPUMP_ADMIN_PASSWORD", "environment-password")
	if err := os.WriteFile("secret", []byte("file-secret-0123456789abcdef0123456789\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RADIOPUMP_JWT_SECRET_FILE", "secret")
	cfg, err := NewConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.AdminPassword != "environment-password" || cfg.Server.JWTSecret != "file-secret-0123456789abcdef0123456789" {
		t.Fatal("не применены источники секретов")
	}
	t.Setenv("RADIOPUMP_JWT_SECRET", "must-not-leak")
	if _, err := NewConfig(); err == nil || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("конфликт источников: %v", err)
	}
}

// Нечитаемые, слишком большие и пустые секреты дают диагностируемую ошибку.
func TestSecretFileFailures(t *testing.T) {
	for name, body := range map[string]string{"empty": "", "too_large": strings.Repeat("x", 4097)} {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir() + "/secret"
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("RADIOPUMP_JWT_SECRET_FILE", path)
			var value string
			err := loadSetting("RADIOPUMP_JWT_SECRET", &value)
			if len(body) > 4096 && err == nil {
				t.Fatal("принят большой секрет")
			}
			if body == "" && (ServerConfig{JWTSecret: value}).ValidateAuth() == nil {
				t.Fatal("принят пустой секрет")
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		t.Setenv("RADIOPUMP_JWT_SECRET_FILE", t.TempDir()+"/missing")
		var value string
		if err := loadSetting("RADIOPUMP_JWT_SECRET", &value); err == nil {
			t.Fatal("отсутствующий файл не вызвал ошибку")
		}
	})
}

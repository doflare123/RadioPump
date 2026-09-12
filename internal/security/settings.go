// Пакет security содержит общие проверки настроек авторизации.
package security

import "strings"

// ValidJWTSecret проверяет минимальную длину ключа HS256. Администратор должен
// сгенерировать случайный секрет: сама по себе длина не гарантирует энтропию.
func ValidJWTSecret(secret string) bool {
	return len(secret) >= 32 && strings.TrimSpace(secret) == secret
}

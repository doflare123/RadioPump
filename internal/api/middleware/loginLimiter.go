package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// NewLoginLimiter ограничивает вход единственного администратора десятью запросами
// в минуту, включая некорректные тела и успешные попытки. Общее состояние исключает
// обход через IP или заголовки и неограниченный рост списка клиентов.
// При перезапуске состояние сбрасывается.
func NewLoginLimiter() func(http.Handler) http.Handler {
	return newLoginLimiter(time.Now)
}

// newLoginLimiter принимает часы для проверки истечения окна без ожидания.
// Мьютекс резервирует попытки до вызова обработчика, в том числе при параллельных запросах.
func newLoginLimiter(now func() time.Time) func(http.Handler) http.Handler {
	var mu sync.Mutex
	var end time.Time
	count := 0
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			current := now()
			if !current.Before(end) {
				end = current.Add(time.Minute)
				count = 0
			}
			allowed := count < 10
			if allowed {
				count++
			}
			retry := int((end.Sub(current) + time.Second - 1) / time.Second)
			mu.Unlock()
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				writeAuthError(w, http.StatusTooManyRequests, "слишком много попыток входа")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

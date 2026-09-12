package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Проверяем подписанные злоумышленником токены напрямую, в обход проверки при старте.
func TestAdminJWTValidation(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, secret, role string
		method             jwt.SigningMethod
		exp                int64
		want               int
	}{
		{"empty", "", "admin", jwt.SigningMethodHS256, time.Now().Add(time.Hour).Unix(), 503},
		{"spaces", strings.Repeat(" ", 32), "admin", jwt.SigningMethodHS256, 0, 503},
		{"short", "weak", "admin", jwt.SigningMethodHS256, 0, 503},
		{"valid", key, "admin", jwt.SigningMethodHS256, time.Now().Add(time.Hour).Unix(), 204},
		{"expired", key, "admin", jwt.SigningMethodHS256, 1, 401},
		{"no expiry", key, "admin", jwt.SigningMethodHS256, 0, 401},
		{"wrong algorithm", key, "admin", jwt.SigningMethodHS384, time.Now().Add(time.Hour).Unix(), 401},
		{"wrong role", key, "listener", jwt.SigningMethodHS256, time.Now().Add(time.Hour).Unix(), 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"role": tc.role}
			if tc.exp != 0 {
				claims["exp"] = tc.exp
			}
			token, err := jwt.NewWithClaims(tc.method, claims).SignedString([]byte(tc.secret))
			if err != nil {
				t.Fatal(err)
			}
			h := NewMiddlewareAdmin(tc.secret).AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
			r := httptest.NewRequest("GET", "/api/auth/me", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
		})
	}
}

// Параллельные запросы не должны превышать лимит окна; поддельные заголовки прокси
// расходуют общий лимит, а истечение окна восстанавливает доступ без продления блокировки.
func TestLoginLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	var admitted atomic.Int32
	h := newLoginLimiter(func() time.Time { return now })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		admitted.Add(1)
		w.WriteHeader(401)
	}))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/", nil)
			r.Header.Set("X-Forwarded-For", "203.0.113.1")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 && (w.Code != 429 || w.Header().Get("Retry-After") != "60") {
				t.Errorf("unexpected response: %d %v", w.Code, w.Header())
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 10 {
		t.Fatalf("admitted %d", admitted.Load())
	}
	now = now.Add(time.Minute)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 401 {
		t.Fatalf("window did not reset: %d", w.Code)
	}
}

package middleware

import "net/http"

type MiddlewareAdmin interface {
	AdminOnly(next http.Handler) http.Handler
}

type middlewareAdmin struct {
}

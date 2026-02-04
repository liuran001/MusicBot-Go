package platform

import "context"

type CookieCheckResult struct {
	OK      bool
	Message string
}

type CookieChecker interface {
	CheckCookie(ctx context.Context) (CookieCheckResult, error)
}

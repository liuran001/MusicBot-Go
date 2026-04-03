package platform

import (
	"context"
	"time"
)

type CookieCheckResult struct {
	OK      bool
	Message string
}

type CookieChecker interface {
	CheckCookie(ctx context.Context) (CookieCheckResult, error)
}

type CookieRenewer interface {
	ManualRenew(ctx context.Context) (string, error)
}

type AutoRenewStatus struct {
	Enabled  bool
	Interval time.Duration
}

type AutoRenewer interface {
	GetAutoRenewStatus(ctx context.Context) (AutoRenewStatus, error)
	SetAutoRenew(ctx context.Context, enabled bool, interval time.Duration) (AutoRenewStatus, error)
}

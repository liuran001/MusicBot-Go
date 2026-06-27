package i18n

import "context"

type ctxKey struct{}

// WithLocalizer returns a child context carrying loc. The router calls this once
// per update after resolving the request language; downstream handlers retrieve
// it via From.
func WithLocalizer(ctx context.Context, loc *Localizer) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if loc == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, loc)
}

// From returns the Localizer stored in ctx. If none was injected (e.g. an
// internal code path that never went through the router), it falls back to the
// default-language Localizer so callers always get a usable, non-nil value.
func From(ctx context.Context) *Localizer {
	if ctx != nil {
		if loc, ok := ctx.Value(ctxKey{}).(*Localizer); ok && loc != nil {
			return loc
		}
	}
	return For(DefaultLanguage)
}

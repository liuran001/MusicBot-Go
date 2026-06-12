package platform

import (
	"context"
	"time"
)

type AccountStatus struct {
	Platform        string
	DisplayName     string
	LoggedIn        bool
	UserID          string
	Nickname        string
	Summary         string
	AuthMode        string
	SessionSource   string
	CanCheckCookie  bool
	CanRenewCookie  bool
	SupportedLogins []string
	ExpiresAt       *time.Time
}

type AccountStatusProvider interface {
	AccountStatus(ctx context.Context) (AccountStatus, error)
}

type CookieImportResult struct {
	Updated bool
	Message string
}

type CookieImporter interface {
	ImportCookie(ctx context.Context, raw string) (CookieImportResult, error)
}

type QRLoginImage struct {
	URL      string
	Base64   string
	PNG      []byte
	FileName string
}

type QRLoginSession struct {
	Platform string
	Image    QRLoginImage
	CancelID string
	Caption  string
	Timeout  time.Duration
	Poll     func(ctx context.Context, onUpdate func(QRLoginUpdate, error))
	Cancel   func()
}

type QRLoginUpdate struct {
	State   string
	Message string
	Final   bool
	Status  *AccountStatus
	Caption string
}

type QRLoginProvider interface {
	StartQRLogin(ctx context.Context) (*QRLoginSession, error)
	CancelQRLogin(ctx context.Context, cancelID string) error
}

type LoginMethodProvider interface {
	SupportedLoginMethods() []string
}

type SignInProvider interface {
	SignIn(ctx context.Context) (string, error)
}

// LanguageProvider is implemented by platforms whose metadata language can be
// viewed and changed at runtime (e.g. Apple Music). ShowLanguage returns a
// human-readable status (current storefront/language + supported languages);
// SetLanguage validates and applies a new language, persisting it.
type LanguageProvider interface {
	ShowLanguage(ctx context.Context) (string, error)
	SetLanguage(ctx context.Context, lang string) (string, error)
}

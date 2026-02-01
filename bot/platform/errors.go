package platform

import (
	"errors"
	"fmt"
)

// Common platform errors that can be checked with errors.Is.
var (
	// ErrNotFound is returned when a track, artist, album, or playlist is not found.
	ErrNotFound = errors.New("platform: resource not found")

	// ErrRateLimited is returned when the platform API rate limit is hit.
	ErrRateLimited = errors.New("platform: rate limit exceeded")

	// ErrUnavailable is returned when content is not available in the current region or context.
	ErrUnavailable = errors.New("platform: content unavailable")

	// ErrUnsupported is returned when a feature is not supported by the platform.
	ErrUnsupported = errors.New("platform: feature not supported")

	// ErrInvalidQuality is returned when the requested audio quality is not available.
	ErrInvalidQuality = errors.New("platform: invalid quality")

	// ErrAuthRequired is returned when authentication is required but not provided.
	ErrAuthRequired = errors.New("platform: authentication required")
)

// PlatformError wraps an error with additional platform-specific context.
// This allows checking the underlying error type using errors.Is and errors.As
// while also providing information about which platform and resource caused the error.
type PlatformError struct {
	// Platform is the name of the platform that returned the error (e.g., "netease", "spotify").
	Platform string

	// Resource is the type of resource that was being accessed (e.g., "track", "artist", "album").
	Resource string

	// ID is the identifier of the resource (if applicable).
	ID string

	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *PlatformError) Error() string {
	if e.ID != "" {
		return fmt.Sprintf("%s: %s %s: %v", e.Platform, e.Resource, e.ID, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Platform, e.Resource, e.Err)
}

// Unwrap implements error unwrapping for errors.Is and errors.As.
func (e *PlatformError) Unwrap() error {
	return e.Err
}

// NewNotFoundError creates a PlatformError for a resource that was not found.
func NewNotFoundError(platform, resource, id string) error {
	return &PlatformError{
		Platform: platform,
		Resource: resource,
		ID:       id,
		Err:      ErrNotFound,
	}
}

// NewRateLimitedError creates a PlatformError for rate limit errors.
func NewRateLimitedError(platform string) error {
	return &PlatformError{
		Platform: platform,
		Resource: "api",
		Err:      ErrRateLimited,
	}
}

// NewUnavailableError creates a PlatformError for unavailable content.
func NewUnavailableError(platform, resource, id string) error {
	return &PlatformError{
		Platform: platform,
		Resource: resource,
		ID:       id,
		Err:      ErrUnavailable,
	}
}

// NewUnsupportedError creates a PlatformError for unsupported features.
func NewUnsupportedError(platform, feature string) error {
	return &PlatformError{
		Platform: platform,
		Resource: feature,
		Err:      ErrUnsupported,
	}
}

// NewInvalidQualityError creates a PlatformError for invalid quality requests.
func NewInvalidQualityError(platform, trackID string, quality Quality) error {
	return &PlatformError{
		Platform: platform,
		Resource: "track",
		ID:       trackID,
		Err:      fmt.Errorf("%w: %s", ErrInvalidQuality, quality.String()),
	}
}

// NewAuthRequiredError creates a PlatformError for authentication errors.
func NewAuthRequiredError(platform string) error {
	return &PlatformError{
		Platform: platform,
		Resource: "api",
		Err:      ErrAuthRequired,
	}
}

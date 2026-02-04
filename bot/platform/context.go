package platform

import "context"

type playlistLimitKey struct{}
type playlistOffsetKey struct{}

// WithPlaylistLimit stores a playlist display limit in context.
// A non-positive limit is ignored.
func WithPlaylistLimit(ctx context.Context, limit int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		return ctx
	}
	return context.WithValue(ctx, playlistLimitKey{}, limit)
}

// PlaylistLimitFromContext retrieves the playlist display limit from context.
// Returns 0 if no limit was set.
func PlaylistLimitFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if value := ctx.Value(playlistLimitKey{}); value != nil {
		if limit, ok := value.(int); ok {
			return limit
		}
	}
	return 0
}

// WithPlaylistOffset stores a playlist offset in context.
// A negative offset is treated as zero.
func WithPlaylistOffset(ctx context.Context, offset int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if offset < 0 {
		offset = 0
	}
	return context.WithValue(ctx, playlistOffsetKey{}, offset)
}

// PlaylistOffsetFromContext retrieves the playlist offset from context.
// Returns 0 if no offset was set.
func PlaylistOffsetFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if value := ctx.Value(playlistOffsetKey{}); value != nil {
		if offset, ok := value.(int); ok {
			return offset
		}
	}
	return 0
}

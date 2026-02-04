package admincmd

import "context"

type Command struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, args string) (string, error)
}

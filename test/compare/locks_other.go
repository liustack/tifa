//go:build !unix

package compare

import (
	"context"
	"errors"
)

// holdLocks needs flock, which this system does not have.
func holdLocks(ctx context.Context, paths []string) (release func(), err error) {
	if len(paths) > 0 {
		return nil, errors.New("lock files need flock, which this system does not have")
	}
	return func() {}, nil
}

package orchestrator

import (
	"context"
	"errors"
)

func isDeadlineExceeded(err error) bool {
	return err != nil && errors.Is(err, context.DeadlineExceeded)
}

func isCanceled(err error) bool { return err != nil && errors.Is(err, context.Canceled) }

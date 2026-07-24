package contextowner

import "context"

type owner struct {
	ctx context.Context
}

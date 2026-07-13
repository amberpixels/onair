package live

import (
	"context"

	"github.com/amberpixels/onair"
)

// Static answers with a ref that was handed in directly - the CLI's
// `--live-ref` pipe form, or a host that already knows what is running.
type Static struct {
	Info onair.LiveInfo
}

func (s Static) Live(context.Context) (onair.LiveInfo, error) {
	return s.Info, nil
}

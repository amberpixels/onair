// Package render turns a Report into output. The core never renders;
// the CLI's tty view and a host's JSON API are two consumers of the same
// Report.
package render

import (
	"encoding/json"
	"io"

	"github.com/amberpixels/onair"
)

// JSON writes the report as indented JSON. The shape is a public API:
// changes are additive only.
func JSON(w io.Writer, r *onair.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

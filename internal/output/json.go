package output

import (
	"encoding/json"
	"io"
)

func WriteJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

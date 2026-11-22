package ses

import (
	"bytes"
	"encoding/json"
)

func newEncoder(buf *bytes.Buffer) *json.Encoder {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	return enc
}

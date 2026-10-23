package sample

import (
	"bytes"
	"errors"

	"github.com/liza-mas/liza/internal/jsonout"
)

// errorEnvelope is the adapter of TestOracleSampleErrorEnvelope: the only
// place this package reaches into production code.
func errorEnvelope() []byte {
	var buf bytes.Buffer
	_ = jsonout.WriteResult(&buf, nil, nil, errors.New("task arm-1 not found"))
	return buf.Bytes()
}

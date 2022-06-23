package logger

import (
	"encoding/json"
	"io"
)

// writeBadRequest writes the json-encoded error message
// to the response with a 400 bad request status code.
func WriteBadRequest(w io.Writer, err error) {
	writeError(w, err, 400)
}

// writeNotFound writes the json-encoded error message to
// the response with a 404 not found status code.
func WriteNotFound(w io.Writer, err error) {
	writeError(w, err, 404)
}

// writeInternalError writes the json-encoded error message
// to the response with a 500 internal server error.
func WriteInternalError(w io.Writer, err error) {
	writeError(w, err, 500)
}

// writeJSON writes the json-encoded representation of v to
// the response body.
func WriteJSON(w io.Writer, v interface{}) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// writeError writes the json-encoded error message to the
// response.
func writeError(w io.Writer, err error, status int) {
	out := struct {
		Message string `json:"error_msg"`
		Status  int    `json:"code"`
	}{err.Error(), status}
	WriteJSON(w, &out)
}

package logger

import (
	"encoding/json"
	"net/http"
	"time"
)

// unix epoch time
var epoch = time.Unix(0, 0).Format(time.RFC1123)

// http headers to disable caching.
var noCacheHeaders = map[string]string{
	"Expires":         epoch,
	"Cache-Control":   "no-cache, private, max-age=0",
	"Pragma":          "no-cache",
	"X-Accel-Expires": "0",
}

// helper function to prevent http response caching.
func nocache(w http.ResponseWriter) {
	for k, v := range noCacheHeaders {
		w.Header().Set(k, v)
	}
}

// writeBadRequest writes the json-encoded error message
// to the response with a 400 bad request status code.
func WriteBadRequest(w http.ResponseWriter, err error) {
	writeError(w, err, 400)
}

// writeNotFound writes the json-encoded error message to
// the response with a 404 not found status code.
func WriteNotFound(w http.ResponseWriter, err error) {
	writeError(w, err, 404)
}

// writeInternalError writes the json-encoded error message
// to the response with a 500 internal server error.
func WriteInternalError(w http.ResponseWriter, err error) {
	writeError(w, err, 500)
}

// writeJSON writes the json-encoded representation of v to
// the response body.
func WriteJSON(w http.ResponseWriter, v interface{}, status int) {
	for k, v := range noCacheHeaders {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// writeError writes the json-encoded error message to the
// response.
func writeError(w http.ResponseWriter, err error, status int) {
	out := struct {
		Message string `json:"error_msg"`
		Status  int    `json:"code"`
	}{err.Error(), status}
	WriteJSON(w, &out, status)
}

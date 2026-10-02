package cli

import (
	"encoding/json"
	"io"
	"net/http"
)

// readAPIError turns a failed response body into an error, reading the whole
// body first so the caller can still drain it.
func readAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return errString(e.Error)
	}
	return errString("HTTP " + resp.Status)
}

// errString lets errors be built in a single expression at call sites.
func errString(msg string) error { return &cliError{msg} }

type cliError struct{ msg string }

func (e *cliError) Error() string { return e.msg }

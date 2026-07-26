// sdworkspace/sdbackend/internal/server/cmd/api/json_helpers.go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type jsonResponse struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// readJSON tries to read the body of a request and converts it into JSON
func (app *Application) readJSON(w http.ResponseWriter, r *http.Request, data any) error {
	maxBytes := 1048576 // 1 megabyte

	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	err := dec.Decode(data)
	if err != nil {
		return err
	}

	err = dec.Decode(&struct{}{})
	if err != io.EOF {
		return errors.New("body must have only a single JSON value")
	}

	return nil
}

// writeJSON takes a response status code and arbitrary data and writes a json
// response to the client
func (app *Application) writeJSON(w http.ResponseWriter, status int, data any, headers ...http.Header) error {
	out, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if len(headers) > 0 {
		for key, value := range headers[0] {
			w.Header()[key] = value
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(out)
	if err != nil {
		return err
	}

	return nil
}

// respondWithJSON writes a structured JSON response using the jsonResponse format.
// It ensures proper headers, error handling, and safe response writing.
//
// This method should always be used for consistent API responses.
func (app *Application) respondWithJSON(w http.ResponseWriter, statusCode int, resp jsonResponse) {
	// Marshal the response into JSON
	responseBody, err := json.Marshal(resp)
	if err != nil {
		// If marshaling fails, fall back to respondWithError
		app.respondWithError(w, fmt.Errorf("failed to marshal JSON response: %w", err), http.StatusInternalServerError)
		return
	}

	// Set content type and status
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Write response
	if _, err := w.Write(responseBody); err != nil {
		app.Logger.Error("Failed to write JSON response", "error", err)
	}
}

// respondWithError wraps an error into the jsonResponse format and sends it as JSON.
func (app *Application) respondWithError(w http.ResponseWriter, err error, statusCode int) {
	app.respondWithJSON(w, statusCode, jsonResponse{
		Error:   true,
		Message: err.Error(),
		Data:    nil,
	})
}

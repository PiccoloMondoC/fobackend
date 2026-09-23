// focodebase/fobackend/internal/server/cmd/api/error_responses.go
package main

import (
	"net/http"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/logging"
)

func (app *Application) serverErrorResponse(logger *logging.Logger, w http.ResponseWriter, r *http.Request, err error) {
	logger = logger.GetLoggerWithContext(r).WithFunctionName("serverErrorResponse")
	logger.Error("server error", "error", err)

	response := map[string]string{"error": "internal server error"}
	if writeErr := app.writeJSON(w, http.StatusInternalServerError, response, nil); writeErr != nil {
		logger.Error("server error response failed", "error", writeErr)
	}
}

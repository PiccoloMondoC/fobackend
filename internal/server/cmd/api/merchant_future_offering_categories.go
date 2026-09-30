// Package main provides the merchant-facing category choices for Future
// Offering creation.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_future_offering_categories.go
//
// GTM:
//
//	Layer: 2.5 Minimal Catalog / Future Offering Classification
//	Release Class: SPINE
//	Reason:
//	  PCDF-M01 asks the merchant for the one category that best defines the
//	  offering. GET /categories/all returns the whole flat taxonomy with no
//	  ancestry, so identically named categories ("Shoes", "Clothing") cannot
//	  be told apart, and merchants cannot read departments. This read returns
//	  exactly the selectable choices, each with its full ancestry.
//
//	  The taxonomy is Platform data, not merchant data: no merchant context
//	  is required. Reads are not audited, matching other merchant workspace
//	  reads.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Keep this a read-only projection; taxonomy governance remains in the
//	category administration handlers.
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
)

type futureOfferingCategoryOptionsResponse struct {
	Categories []data.FutureOfferingCategoryOption `json:"categories"`
}

// ListFutureOfferingCategoryOptionsHandler returns selectable leaf
// categories with their ancestry.
func (app *Application) ListFutureOfferingCategoryOptionsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	// Same in-handler authorization as the category handlers in
	// departments.go, independent of route middleware.
	if !app.HasPermission(ctx, listCategoriesAction) {
		app.respondWithError(w, errors.New("forbidden: insufficient permissions"), http.StatusForbidden)
		return
	}

	options, err := app.Models.Category.ListFutureOfferingCategoryOptions(ctx, &app.Models.Department)
	if err != nil {
		app.serverErrorResponse(app.Logger.GetLoggerWithContext(r), w, r, err)
		return
	}
	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Category choices retrieved",
		Data:    futureOfferingCategoryOptionsResponse{Categories: options},
	})
}

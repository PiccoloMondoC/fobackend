// Package main provides self-scoped Merchant Account context discovery.
//
// focodebase/fobackend/internal/server/cmd/api/merchant_accounts_mine.go
//
// GTM:
//
//	Layer: 2.2 Identity / Auth Domain
//	Release Class: SPINE
//	Reason:
//	  Merchant Account context discovery is release-critical authenticated
//	  identity infrastructure. It exposes the canonical active Merchant Account
//	  and Merchant identifiers under which an authenticated principal may
//	  operate, without deriving Merchant authority from public role identity.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve self-scoped discovery through the authenticated principal.
//	Preserve canonical Merchant Account and Merchant identifiers.
//	Preserve active-account filtering through MerchantAccountModel.
//	Preserve the merchant_contexts response envelope consumed by authenticated
//	merchant clients.
//	Do not fabricate Merchant context from role identity, JWT claims, local
//	client state, or caller-supplied identifiers.
//	Block deployment if this file breaks authenticated Merchant Account context
//	discovery or returns contexts the principal does not canonically own.
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/google/uuid"
)

type ownMerchantAccountContext struct {
	MerchantAccountID uuid.UUID `json:"merchant_account_id"`
	MerchantID        uuid.UUID `json:"merchant_id"`
	AccountStatus     string    `json:"account_status"`
}

// ownMerchantAccountsResponseData is the canonical data payload for self-scoped
// Merchant operating-context discovery. MerchantContexts is always emitted as
// a JSON array, including when no active context exists.
type ownMerchantAccountsResponseData struct {
	MerchantContexts []ownMerchantAccountContext `json:"merchant_contexts"`
}

// buildOwnMerchantContextsResponse maps persisted active Merchant Accounts into
// the stable merchant_contexts response contract consumed by merchant clients.
// It creates no authority; the supplied accounts have already been resolved by
// the canonical principal-scoped persistence query.
func buildOwnMerchantContextsResponse(accounts []*data.MerchantAccount) ownMerchantAccountsResponseData {
	contexts := make([]ownMerchantAccountContext, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		contexts = append(contexts, ownMerchantAccountContext{MerchantAccountID: account.ID, MerchantID: account.MerchantID, AccountStatus: string(account.AccountStatus)})
	}
	return ownMerchantAccountsResponseData{MerchantContexts: contexts}
}

// GetOwnActiveMerchantContextsHandler returns only active Merchant Accounts
// for which the authenticated caller is the canonical principal.
func (app *Application) GetOwnActiveMerchantContextsHandler(w http.ResponseWriter, r *http.Request) {
	logger := app.Logger.GetLoggerWithContext(r).WithFunctionName("GetOwnActiveMerchantContextsHandler")
	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()
	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(w, errors.New("unauthorized"), http.StatusUnauthorized)
		return
	}
	accounts, err := app.Models.MerchantAccount.ListActiveForPrincipal(ctx, *userID)
	if err != nil {
		app.serverErrorResponse(logger, w, r, err)
		return
	}
	app.respondWithJSON(w, http.StatusOK, jsonResponse{Error: false, Message: "Own merchant operating contexts retrieved successfully", Data: buildOwnMerchantContextsResponse(accounts)})
}

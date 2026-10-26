// Package main provides bounded merchant capabilities derived from canonical active principal ownership.
// focodebase/fobackend/internal/server/cmd/api/merchant_principal_permissions.go
//
// GTM:
//     Layer: 3.2 Merchant Registration / HTTP and Authorization
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
// CE compatibility bridge between role-based coarse permissions and canonical
// merchant principal authority. Only the listed existing merchant capabilities
// participate; no billing, administration or generic permission bypass exists.
package main

import (
    "context"

    "github.com/google/uuid"
)

func isMerchantPrincipalCapability(name string) bool {
    switch name {
    case "read_merchant", "create_merchant_future_offering", "list_merchant_future_offerings",
        "read_merchant_future_offering", "update_merchant_future_offering_draft",
        "discard_merchant_future_offering_draft", "list_engagement_actions", "list_categories":
        return true
    }
    return false
}

func (app *Application) hasMerchantPrincipalCapability(ctx context.Context, name string) bool {
    // AuthMiddleware establishes identity and the live platform role. Active
    // merchant ownership is independent of that role; it grants only the
    // capabilities above for the authenticated actor's own merchant account.
    if !isMerchantPrincipalCapability(name) {
        return false
    }
    actor := app.getUserIDFromContext(ctx)
    if actor == nil || *actor == uuid.Nil {
        return false
    }
    account, err := app.Models.MerchantAccount.GetByPrincipal(ctx, *actor)
    if err != nil || account == nil {
        return false
    }
    // Platform catalogs are shared read-only data. Merchant context is
    // irrelevant to these reads, including any placeholder inserted by the
    // development fallback middleware. Authority still requires the actor's
    // own active canonical account below; no selected merchant grants it.
    catalogRead := name == "list_engagement_actions" || name == "list_categories"
    if !catalogRead {
        selected := app.getMerchantIDFromContext(ctx)
        if selected == nil || *selected == uuid.Nil || *selected != account.MerchantID {
            return false
        }
    }
    owned, err := app.Models.MerchantAccount.IsPrincipalForMerchant(ctx, *actor, account.MerchantID)
    if err != nil || !owned {
        return false
    }
    // Reuse the managed merchant capability catalog without assigning a role
    // or inheriting its unrelated billing or administrative permissions.
    role, err := app.Models.Role.GetRoleByName(ctx, "merchant")
    if err != nil || role == nil || !role.IsActive || role.DeletedAt != nil {
        return false
    }
    allowed, err := app.Models.RolePermission.RoleHasPermission(ctx, role.ID.String(), name)
    return err == nil && allowed
}

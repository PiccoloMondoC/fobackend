// Package data provides the Foundation Level 4 merchant workspace persistence boundary.
// focodebase/fobackend/internal/data/merchant_center.go
//
// GTM:
//     Layer: 2.1 Persistence / Merchant Registration
//     Release Class: SPINE
//     Reason: Supports the complete Foundation Level 4 merchant-registration and account-navigation vertical.
//
// SPINE Rule:
//     Keep compiling and preserve established security, lifecycle, and privacy contracts.
//     Preserve canonical principal ownership and backend authorization.
//     Keep private registration values out of public projections, logs, and errors.
//
// Registration persistence belongs to merchant_registrations.go; canonical
// merchant operations remain governed by MerchantAccount principal ownership.
// Public storefronts, multiuser memberships and investigations remain deferred.
package data

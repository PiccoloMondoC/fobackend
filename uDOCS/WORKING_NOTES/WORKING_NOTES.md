## To update code on Github:
git add -A
git commit -m "rewrite merchant_future_offering_billing_periods.go data layer"
git push

| data layer | handler layer | services layer |

### To Test
gofmt -w ./internal/data/errors.go

### Vertical Build Strategy
Our strategy is  to build vertically across each of the 27 files listed below, not horizontally. Completing one file across data → handlers → services → startup → reviews means each slice reaches a genuine state of completion before we move on.

By the time we finish the 27th Future Offering file, we won't have 27 partially built components—we'll have a coherent, production-grade subsystem awaiting integration testing.


### Document Context map
[SSD]
Why does Sagrenti exist?
        │
        ▼
SPA
What platform must exist to fulfill that strategy?
        │
        ├───────────────┐
        ▼               ▼
SEA                    FCDS
How do we execute?     How do we compete and defend?
        │               │
        └───────┬───────┘
                ▼
        Commerce Architecture
                │
        ┌───────┼────────┐
        ▼       ▼        ▼
      CMOA     FCP      MCBS
   Objects &   Future   Revenue
relationships Commerce model
        │       │        │
        └───────┼────────┘
                ▼
              BEG
 How engineers must implement all of it
                │
                ▼
 Future Offering Experience Architecture
 Information architecture, journeys,
 page hierarchy and interaction rules
                │
                ▼
       Angular Frontend Architecture
 Routes, components, services, state,
 APIs, permissions and design system





gofmt -w \ ./internal/data/merchant_platform_credit_accounts.go

go vet ./internal/data/...

go vet ./internal/services/...
go vet ./internal/server/cmd/api/...

go test ./internal/data
go test ./internal/services
go test ./internal/server/cmd/api



Before you decide whether or not to write a prompt, please first ask yourself whether the SE stage will genuinely discover something new. If the answer is yes, write the prompt for SE. Otherwise, skip the prompt and I go straight to the Chief Engineer implementation.

# Projected Project Schedule:
* Backend feature complete                        — October 2026
* Frontend feature complete                       — June 2027
* System integration & architecture validation    — July–August 2027
* Production deployment                           — September 2027


> Is there a way to make the website available for a select few before a launch day?

2. Invite-only accounts

Only approved users can sign in. Everyone else sees a launch page.

Good for:

controlled merchant onboarding;
beta testing;
tracking who has access;
removing access individually.

This is usually the best option for Sagrenti.

# My recommendation for Sagrenti
Use two layers:

Public visitors
    ↓
Launch / waitlist page

Approved users
    ↓
Authentication
    ↓
Private pre-launch platform

You can call the phases:
* Internal Preview
* Private Alpha
* Invite-Only Beta
* Public Launch


The public launch does not require replacing the website. You simply change configuration so public registration or public routes become available. That fits your doctrine well: engineering provides the access-control capability, while Administration controls whether the platform is private, invite-only, or public.


Sagrenti does not disclose consumer identity to merchants as part of Future Offering engagement. Consumer identity remains under the consumer's control. When a Future Offering reaches a merchant-facing commercial stage, Sagrenti issues the consumer an authenticated engagement credential that the consumer may present to the merchant to establish a direct commercial relationship.


Request an Early Access Pass
Generate Pre-order Pass

Our privacy posture would not allow us to give info about any consumer to a merchant. Only the consumer should be able to reach out to the merchant, not the other way around using a digital token. The token might be represented as:

* QR code
* Secure URL
* Digital credential
* One-time authorization code



Early Access
Reservation
Pre-order
Beta participation
Event admission
Launch-day priority
Promotional entitlement




The SE review should serve as an input into your own independent CE review. Your review should challenge the completed slice against the canonical table and the underlying doctrine, with special attention to any drift introduced while the architecture evolves.



I agree with SE’s overall verdict, but I’m tightening two points before issuing code: the merchant/billable-event ownership check needs an explicit transaction-aware primitive, and the stale DB-error contract must be removed so handlers/services cannot inherit a false guarantee. I’m also folding in the small lifecycle-validation, logging, and deterministic-read fixes now so this slice is genuinely closed before we move upward.


I’ve got the SE review now. I’m doing the independent CE pass against the completed handler, the canonical data contract, and the current Commerce/Payments doctrine—not just accepting SE’s “documentation-only” conclusion at face value.


BEG §18.6D requires that we ask ourselves: Does this implementation unnecessarily couple domains in a way that would make later service extraction require redesign? Please read BEG §18.6D.
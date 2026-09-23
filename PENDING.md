ge-auth/
│── cmd/
│   ├── api/                  # Main API entry point
│   │   ├── main.go           # Starts the HTTP server
│   │   ├── router.go         # Registers HTTP routes
│   │   ├── middleware.go     # Middleware (Auth, Logging, etc.)
│   │   ├── config.go         # Configuration management
│── internal/
│   ├── server/
│   │   ├── handlers/         # HTTP Handlers
│   │   │   ├── client.go     # Client registration handlers
│   │   │   ├── auth.go       # Authorization handlers
│   │   │   ├── token.go      # Token handlers (issue, revoke, introspect)
│   │   │   ├── userinfo.go   # OpenID Connect user info handler
│   │   │   ├── audit.go      # Audit logging handlers
│   ├── data/
│   │   ├── models/           # Database models
│   │   │   ├── client.go     # OAuth2 client model
│   │   │   ├── auth_code.go  # Authorization code model
│   │   │   ├── token.go      # Token model (Access & Refresh)
│   │   │   ├── user.go       # User model (Optional for OpenID Connect)
│   │   │   ├── consent.go    # User consent model
│   │   ├── repositories/     # Data access layer (CRUD operations)
│   │   │   ├── client_repo.go
│   │   │   ├── auth_repo.go
│   │   │   ├── token_repo.go
│   │   │   ├── user_repo.go
│   │   │   ├── audit_repo.go
│   ├── services/
│   │   ├── oauth2/           # OAuth2 Core Business Logic
│   │   │   ├── client.go     # Client management service
│   │   │   ├── auth.go       # Authorization & consent service
│   │   │   ├── token.go      # Token management service
│   │   │   ├── introspect.go # Token introspection logic
│   │   │   ├── revoke.go     # Token revocation logic
│   │   │   ├── userinfo.go   # OpenID Connect user info service
│   │   ├── audit/            # Audit Logging Service
│   │   │   ├── audit.go
│   ├── security/
│   │   ├── jwt.go            # JWT signing & verification
│   │   ├── hash.go           # Secure client secret storage
│   │   ├── auth_middleware.go # Middleware for token validation
│   ├── utils/
│   │   ├── config.go         # Configuration loader
│   │   ├── logger.go         # Central logging utility
│   │   ├── errors.go         # Custom error handling
│   │   ├── http.go           # HTTP helper utilities
│── migrations/
│   ├── 0001_init.sql         # Initial database schema
│   ├── 0002_add_oauth2.sql   # OAuth2-related table updates
│── tests/
│   ├── integration/          # API integration tests
│   │   ├── oauth2_test.go
│   ├── unit/                 # Unit tests for services and handlers
│   │   ├── token_service_test.go
│   │   ├── auth_service_test.go
│── deployments/
│   ├── docker-compose.yml    # Local testing
│   ├── k8s/                  # Kubernetes manifests for cloud deployment
│── Makefile                  # Common build & test commands
│── README.md                 # Documentation


Breakdown of Key Components
1. cmd/api/ (API Entry Point)
main.go → Starts the OAuth2 server.

router.go → Registers routes and middleware.

middleware.go → Defines authentication, logging, and security middleware.

config.go → Loads environment variables and configurations.

2. internal/server/handlers/ (HTTP Handlers)
client.go → Handles client registration (POST /oauth2/register).

auth.go → Handles user authentication and consent (GET /oauth2/authorize).

token.go → Issues, refreshes, and revokes tokens (POST /oauth2/token).

userinfo.go → Implements OpenID Connect (GET /oauth2/userinfo).

audit.go → Handles audit logging endpoints (GET /audit/oauth2/logs).

3. internal/data/models/ (Database Models)
client.go → Defines the OAuth2 client structure (client_id, secret, redirect_uris).

auth_code.go → Stores issued authorization codes.

token.go → Stores access and refresh tokens.

user.go → User entity for OpenID Connect.

consent.go → Stores user consent history.

4. internal/data/repositories/ (Database Repositories)
client_repo.go → CRUD operations for OAuth2 clients.

auth_repo.go → Handles authorization codes.

token_repo.go → Manages token storage, retrieval, and validation.

audit_repo.go → Stores logs for auditing purposes.

5. internal/services/oauth2/ (OAuth2 Business Logic)
client.go → Implements client registration and retrieval.

auth.go → Handles user authentication, consent, and code issuance.

token.go → Manages access token issuance and validation.

introspect.go → Implements token introspection.

revoke.go → Handles token revocation.

userinfo.go → Retrieves authenticated user information.

6. internal/security/ (Security Utilities)
jwt.go → JWT generation, signing, and validation.

hash.go → Secure password and client secret hashing.

auth_middleware.go → Middleware for validating OAuth2 tokens.

7. internal/utils/ (Utility Functions)
config.go → Loads application configurations.

logger.go → Logging utility for audit and debugging.

errors.go → Centralized error handling.

http.go → Helper functions for HTTP requests.

8. migrations/ (Database Migrations)
0001_init.sql → Creates base database tables.

0002_add_oauth2.sql → Adds OAuth2-specific tables.

9. tests/ (Unit & Integration Testing)
integration/oauth2_test.go → Full OAuth2 flow tests.

unit/token_service_test.go → Token validation logic.

unit/auth_service_test.go → Authentication and consent handling.

10. deployments/ (Deployment Configs)
docker-compose.yml → Local Docker setup for testing.

k8s/ → Kubernetes manifests for cloud deployments.

Advantages of this Structure
✅ Separation of Concerns → Clear distinction between API, business logic, database, and security.
✅ Scalability → Easily extend OAuth2 functionality (PKCE, Device Flow, etc.).
✅ Security Focus → Token management, encryption, and compliance baked in.
✅ Maintainability → Unit-tested services, modular design, and structured handlers.
✅ Deployment-Ready → Supports Docker, Kubernetes, and CI/CD pipelines.

This layout ensures robust OAuth2 integration while maintaining clean architecture, security, and scalability.



✅ User Account Deletion Policy

User-initiated account deletion = soft delete on users
This triggers soft delete cascades to all subordinate data: wallet, profile, settings, notifications, favorites.

Admin expulsion = same flow
Admins should not delete wallets or profiles directly, only mark the user as soft-deleted or suspended. That drives downstream effects automatically.

Hard delete policy
Only after a retention period (e.g. 10 years) and triggered by a scheduled archival process—not manual interaction.

✅ Checklist: Safe User Deletion Architecture
🔒 Policy Enforcement
 Only DELETE /me (user-initiated) or DELETE /users/{id} (admin-initiated) soft-deletes the user.

 Disallow DELETE or PATCH routes for wallets, settings, profiles directly.

 Require high-trust roles (e.g. admin, compliance_officer) to trigger user deletion.

🔁 Cascade Soft-Delete Design
 When a user is soft-deleted, cascade soft-delete to:

 User Profile

 User Wallet

 User Settings

 User Notifications

 User Favorites

 Any other user-owned resources (e.g., preferences, consent logs)

📦 Data Modeling
 Add deleted_at timestamp (or soft delete flag) to all user-linked tables.

 Add foreign keys with ON DELETE SET NULL or restrict deletion if relationships matter.

🧾 Audit Logging (Only at the handler level. Never at the model level)
 Log the initiating actor, time, and reason for user deletions (including cascaded deletions).

 Prevent hard deletes except by scheduled archival job.

 Include audit log entries in all cascaded deletions.

📆 Archival/Hard Delete Job
 Create a background task that:

 Identifies users soft-deleted for ≥10 years.

 Validates audit compliance.

 Runs irreversible hard-delete with irreversible audit log write.

🔄 Flowchart: User Deletion Process
plaintext
Copy
Edit
             ┌────────────┐
             │User/Admin  │
             │Triggers    │
             │Deletion    │
             └────┬───────┘
                  │
        ┌─────────▼──────────┐
        │ Check Permission   │◄───── Admin must have 'expel_user'
        │ & Extract UserID   │
        └─────────┬──────────┘
                  │
        ┌─────────▼──────────┐
        │ Soft Delete        │
        │ User (deleted_at)  │
        └─────────┬──────────┘
                  │
        ┌─────────▼──────────┐
        │ Cascade to:        │
        │ - Wallet           │
        │ - Profile          │
        │ - Settings         │
        │ - Notifications    │
        │ - Favorites        │
        └─────────┬──────────┘
                  │
        ┌─────────▼──────────┐
        │ Audit all actions  │
        └─────────┬──────────┘
                  │
        ┌─────────▼──────────┐
        │ Block login / API  │
        │ access to user     │
        └────────────────────┘



✅ Why UTC for All Stored Timestamps
Reason	Benefit
Consistency:	Prevents confusion from time zone drift, DST changes, or regional discrepancies
Reversibility:	UTC is a global baseline—local time can always be derived from it
Portability:	Databases, logs, and backups are usable in any context without translation
Compliance:	Critical for audit trails, legal recordkeeping, and traceability
Debugging:	Ensures logs from distributed systems align correctly


✅ What to Do
Always use time.Now().UTC() for:

CreatedAt / UpdatedAt / DeletedAt

Token expirations

Audit timestamps

Background job schedules

Any persisted datetime

Localize only at render/report time using the user’s time zone offset or preference.


# SPONSORSHIPS

## SoftDeleteOfferSponsorship:
The question arises: Is soft deleting active sponsorships necessary? Below is a clear summary of the analysis;:

## When SoftDelete is not necessary:
* The sponsorship is time-bound, not auto-renewed.
* Once end_date passes, the sponsorship is simply considered inactive.
* No business logic requires mid-term cancellation or removal from UI/queries before the natural expiry.

## When SoftDelete is useful:
**Early termination:** Merchant wants to cancel the sponsorship before end_date.
**UI control:** You want to hide active but unwanted sponsorships from user views.
**Error correction:** Staff or merchant made a mistake, and the record shouldn't remain visible/active even temporarily.
**Audit trail:** You want to track deactivations distinct from expiry (e.g., for refunds or disputes).
**Future reactivation:** A soft-deleted record could optionally be restored later.

## Recommendation:
If the business logic doesn’t require early termination, manual hiding, or undo capability, then deleted_at is redundant and you can rely solely on start_date / end_date.

For now we keep the model level SoftDelete method — it costs little and adds flexibility.


## ✅ InsertOffer: Semi-Automated with Human Oversight
Step	                      Description
1. Data Ingestion    Offers pulled from 3rd-party APIs automatically (e.g., merchant feeds)
2. AI Curation	      AI ranks, filters, or enriches the offers (e.g., image enhancement, product deduplication)
3. Editorial Review	Human editors approve, reject, or edit candidate offers
4. Final Insert	   Approved offers are inserted into the database via InsertOffer()

🟢 Insert is not user-driven, but not fully autonomous either — it's editorial-verified automation.


## ✅ OfferPriceHistory: Fully Automated
Recorded exclusively via automation (price polling jobs, merchant updates)

No human interaction needed at all

Trigger alerts and analytics in the background


## 🧠 Recommendation
Operation	Entry Point Type	Access	Automation Level
InsertOffer	Internal service	Curator/editor	Semi-automated
UpdateOffer	Internal service	Editor only	Optional, post-approval
InsertPriceHistory	Internal service	Automation	Fully automated
TriggerPriceDrop	Internal service	Automation	Fully automated


## ✅ Updated Step 2: AI Curation (No Trust Score)
Function	          Role
Filtering	       Discard low-quality or duplicate offers automatically
Ranking	          Prioritize offers based on discount, recency, merchant
Enrichment	       Improve titles/descriptions, normalize categories
Image Enhancement	 Standardize image dimensions, detect broken links
Deduplication	    Match against existing offers using heuristics or embeddings


## If you'd like, I can help you document or scaffold this AI curation phase into:

A standalone service (offer-curator)

An offline enrichment job

Or an editor-facing UI showing “AI-suggested” offers

Just say the word.
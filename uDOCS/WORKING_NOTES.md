## To update code on Github:
git add -A
git commit -m "write merchant_platform_credit_accounts_internal.go services layer"
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



BEG §18.6A requires engineering to implement complete, production-ready platform capabilities, and define the safe operating boundaries while Administration governs their commercial and operational behavior through configuration within those boundaries. Engineering must implement capability—not operational policy. Hard-coded commercial, operational, or policy gating is therefore inappropriate in any layer unless it enforces invariant system safety, security, or data integrity. Where governing documents conflict, SBD and MPA supersede the older MCBS.


 Engineering builds complete capabilities and defines the safe operating boundaries. Administration configures how those capabilities are commercially and operationally used within those boundaries.


The correct handler-layer completion boundary for this project is:

* the domain handler file;
* canonical route registration;
* permission, action, and entity-type seed registration;
* role-permission assignment;
* Admin Console domain declaration where the capability is genuinely administrative;
* handler-specific shared registration required for discoverability.

Only service construction, service-handler integration, and startup composition remain deferred to the service-layer phase.



When we're reviewing handlers, we want the engineer thinking about:

HTTP contracts
request validation
authorization
Admin Console domain declaration where the capability is genuinely administrative
routing
audit
discoverability
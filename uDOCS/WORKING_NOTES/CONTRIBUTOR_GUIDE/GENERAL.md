### HANDLER-LAYER COMPLETION
The correct handler-layer completion boundary for this project is:

* the domain handler file;
* canonical route registration;
* permission, action, and entity-type seed registration;
* role-permission assignment;
* Admin Console domain declaration where the capability is genuinely administrative;
* handler-specific shared registration required for discoverability.

Only service construction, service-handler integration, and startup composition remain deferred to the service-layer phase.

# BETTER
Service implementation, service-handler integration, service construction, and startup composition remain deferred to the service-layer phase.


**When we're reviewing handlers, we want the engineer thinking about:**

HTTP contracts
request validation
authorization
Admin Console domain declaration where the capability is genuinely administrative
routing
audit
discoverability


### SERVICE-LAYER COMPLETION
Service implementation, service-handler integration, service construction, and startup composition form one coherent service-layer completion boundary.

They should be reviewed together because each proves a different part of the same operational chain:

handler call
    → service contract
    → service implementation
    → dependency construction
    → startup wiring
    → executable application

Splitting that chain into separate reviews would add ceremony without creating a meaningful architectural boundary.

# BEG

BEG §18.6A requires engineering to implement complete, production-ready platform capabilities, and define the safe operating boundaries while Administration governs their commercial and operational behavior through configuration within those boundaries. Engineering must implement capability—not operational policy. Hard-coded commercial, operational, or policy gating is therefore inappropriate in any layer unless it enforces invariant system safety, security, or data integrity. Where governing documents conflict, SBD and MPA supersede the older MCBS.


 Engineering builds complete capabilities and defines the safe operating boundaries. Administration configures how those capabilities are commercially and operationally used within those boundaries.


# ROUTES.GO

Within routes.go, use string literals for permission names unless the entire file is intentionally migrated to a different convention. Do not introduce isolated constant-based permission references into otherwise string-literal route registrations.



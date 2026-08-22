One caveat remains: internal/data passed its existing tests, but we still do not yet have the dedicated Postgres concurrency/integration tests for Service Term replacement, lifecycle bypass, duplicate proposal/establishment races, and similar cases. As CE you promised to treat those as required before declaring the entire Service Term slice production-complete, saying but they do not block us from starting the handler layer now.

On ompletion of the service layer, CE had this to say below:

### 7. Production-completion status

I now consider the service implementation and handler/service composition structurally complete.

I still do not declare the Service Term slice production-complete.

The promised PostgreSQL tests remain release requirements, especially:

duplicate proposal race;
duplicate establishment race;
competing replacements;
lifecycle-trigger bypass;
replacement rollback atomicity;
historical immutability;
partial-index behavior;
establishment racing proposal update;
replacement racing proposal update;
administrative narrowing before establishment/replacement.

BEG itself says database-required workflow/reference integrity must be treated as part of the production contract rather than assumed from compilation.
Earlier, I suggested merchant_program_subscription_periods as a minimal table. After studying these three models together, I believe it deserves to be treated as a first-class Commerce entity, not merely a helper table.

That means it should eventually receive:

its own complete data model,
service,
handlers,
startup wiring,
review,
and lifecycle documentation,

just like the other Commerce SPINE models.

Fortunately, that does not change the design we just completed. It simply elevates it to a proper vertical rather than a supporting implementation detail.
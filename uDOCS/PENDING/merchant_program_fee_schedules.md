### merchant_program_fee_schedules_internal.go
### merchant_program_fee_schedules_async.go

The service contract is ready for later billing consumers and for synchronous startup enforcement once the canonical, non-monetary requirement set is approved. This is to say:

### Merchant Program Fee Schedule Readiness

Merchant program fee schedules shall not be treated as startup-ready based solely on table population. Startup readiness shall be evaluated only against Sagrenti's canonical Required Fee Policy Set, which defines which fee categories must exist. The Required Fee Policy Set governs policy coverage only and never specifies monetary values.


It means the service can check whether the pricing rules needed by the platform exist, but we have not yet defined the official list of pricing-rule categories that must exist.

A **non-monetary requirement set** would say things like:

Standard plan must have a monthly subscription-fee schedule.
Premium plan must have a monthly subscription-fee schedule.
A global Future Offering fee schedule must exist.
A global Campaign Performance Fee schedule must exist.

It would **not** say:

Standard costs $49.
Future Offering fee is $25.
Campaign Performance Fee is 8%.

The first list defines **which fee policies are required**. The actual amounts remain operational commercial data stored in `merchant_program_fee_schedules`.
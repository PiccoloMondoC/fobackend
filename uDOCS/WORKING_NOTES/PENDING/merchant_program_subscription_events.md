# What we will do later

Before completing the services slice for subscription events, I will issue authoritative changes to merchant_program_subscriptions.go that add transaction-compatible versions of its mutation methods, such as:

`ActivateTx`(...)
`PauseTx`(...)
`SuspendTx`(...)
`CancelTx`(...)
`ExpireTx`(...)
`UpdatePlanTx`(...)

Then the service will:

Begin transaction
    Update subscription using PauseTx
    Insert event using InsertTx
Commit transaction

If either database action fails, the transaction rolls back both.

## What you should do now

For this data-file step:

1. Use the authoritative merchant_program_subscription_events.go replacement.
2. Add its models.go wiring.
3. Change the migration foreign key to ON DELETE RESTRICT.
4. Add the two recommended indexes.
5. Do not change merchant_program_subscriptions.go yet.

The transaction-compatible subscription changes belong in the upcoming integration work. **I will provide the exact code when we reach that step; you are not being asked to design or infer it yourself**.
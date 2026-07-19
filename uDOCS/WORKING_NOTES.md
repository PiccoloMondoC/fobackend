## To update code on Github:
git add -A
git commit -m "write merchant_program_fee_schedules_internal.go and merchant_program_fee_schedules_async.go services layer"
git push

| data layer | handler layer | services layer |

### To Test
gofmt -w ./internal/data/errors.go

### Vertical Build Strategy
Our strategy is  to build vertically across each of the 27 files listed below, not horizontally. Completing one file across data → handlers → services → startup → reviews means each slice reaches a genuine state of completion before we move on.

By the time we finish the 27th Future Offering file, we won't have 27 partially built components—we'll have a coherent, production-grade subsystem awaiting integration testing.

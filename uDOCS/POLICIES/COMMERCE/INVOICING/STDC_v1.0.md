## Service Term Duration & Calendar Window Doctrine — Agreed Summary

We agreed that the meaning of a Service Term's duration is an **Engineering invariant**, not configurable commercial policy. Administration may determine permitted operating ranges, but it cannot redefine what “one month” or “one year” means.

### 1. Calendar window convention

Service Term windows are **start-inclusive and end-exclusive**:

`[term_starts_on, term_ends_on)`

Thus:

`[Jan 15, Feb 15)` = Jan 15 through Feb 14, **up to but not including Feb 15**.

This eliminates any need to represent a fictitious final `23:59:59.999...` instant and allows adjacent windows without gaps or overlaps:

`[Jan 15, Feb 15)`
`[Feb 15, Mar 15)`

### 2. A month means a calendar month

`duration_months = N` means **N calendar-month advances from the original Service Term anchor**.

It does **not** mean a fixed number of days.

30/360, Actual/360, Actual/365 and similar financial day-count conventions do not define Service Term duration.

### 3. Ordinary day-of-month anchoring

When a Service Term starts on an ordinary numbered calendar day, that original day remains its anniversary anchor.

Example:

`Jan 30 → Feb 28 → Mar 30 → Apr 30 → May 30`

February cannot provide the 30th, so its month-end is a temporary substitute. **February 28 does not become the new anchor.**

### 4. Month-end anchoring

If the authoritative Service Term start date is the **last day of its month**, its anchor is simply **month-end**.

Examples:

`Jan 31 → Feb 28 → Mar 31 → Apr 30 → May 31`

`Feb 28, 2027 → Mar 31 → Apr 30`

`Feb 29, 2028 → Mar 31 → Apr 30`

The invariant is not “28th,” “29th,” “30th,” or “31st.” It is:

> **Month-end remains month-end.**

### 5. Years

One calendar year is definitionally:

> **12 calendar months.**

There is no separate competing annual calendar arithmetic.

### 6. Leap years

Leap years require no special Service Term doctrine.

The existing calendar-month and month-end rules naturally handle February 28/29 and leap-year transitions.

### 7. Dates, not elapsed timestamps

Service Term calendar boundaries are **domain dates**, not quantities of elapsed hours or seconds.

A month is therefore not converted into something such as 720, 730, or 744 hours.

This keeps daylight-saving changes, variable month lengths, and timestamp precision from changing the commercial meaning of a Service Term.

### 8. UTC for machine instants

The Service Term itself remains date-based.

Where another system operation requires a calendar boundary to be translated into an exact machine instant, **UTC is the authoritative system timezone**.

The application server's local timezone must never silently determine Service Term semantics.

### 9. Always derive from the original anchor

Future boundaries must always be calculated from the **original authoritative Service Term start anchor**, not recursively from the previous calculated boundary.

Correct:

`Jan 30 + 1 month → Feb 28`
`Jan 30 + 2 months → Mar 30`
`Jan 30 + 3 months → Apr 30`

Incorrect:

`Jan 30 → Feb 28 → Mar 28 → Apr 28`

A short-month substitution must never cause anniversary drift.

### 10. Established windows are historical facts

Once a Service Term has been validly established, its authoritative calendar window must not be silently recalculated because Administration later changes operating configuration.

For example, if a merchant validly establishes a 60-month Service Term while the permitted range is 1–108 months, subsequently reducing the permitted maximum to 36 months does **not** alter that existing 60-month term.

The governing principle is:

> **Today's rules govern today's decisions; they do not rewrite yesterday's valid facts.**

If an established Service Term must legitimately change, that change requires an **explicit, history-preserving domain operation** rather than silently rewriting the original term.

---

### Core Engineering invariant

All of this can ultimately be reduced to one rule:

> **A Service Term is an immutable, date-based calendar window derived from its original calendar anchor according to deterministic Sagrenti calendar semantics.**

Administration governs which durations merchants may choose **within Engineering's safe boundaries**. Engineering owns what those durations mathematically mean.

With this doctrine settled, **Service Period generation can now consume an authoritative Service Term window rather than inventing its own calendar rules.**

**An established Service Period is never shortened, extended, re-anchored, or repartitioned because of a merchant Service Term amendment. Term changes take effect only at Service Period boundaries.**

**A Service Term amendment may be requested and recorded while a Service Period is performing, but neither Service Term authority nor Service Period authority changes until that Service Period completes. The successor Service Term and its first Service Period become authoritative together at the next Service Period boundary.**

Service Periods are monthly Engineering invariants generated according to STCD. Merchants cannot amend Service Periods. They amend Service Terms. An already-begun Service Period is immutable. A Service Term amendment requested during that period takes effect at its next Service Period boundary.

Service Periods are created just in time. Only the currently performing Service Period, plus historical completed periods, need to exist as authoritative records.

Service Period records describe actual or current service performance windows. They are not a forecast of future service delivery.

Service Periods are created just in time as actual performance windows. They are not a pre-generated forecast schedule.

At a Service Term replacement boundary, the successor Service Term must start exactly at the end boundary of the predecessor’s performing Service Period, and its first Service Period must start at that same boundary. The Service Term transition, successor Period 1 creation, and producer-owned outbox fact must commit atomically.



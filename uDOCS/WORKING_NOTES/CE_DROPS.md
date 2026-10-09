That's exactly the right approach. 😊 I've been treating the entire implementation as one enormous deliverable when I should have divided the engineering work, not lowered the engineering standard.
I propose four self-contained CE drops, each including its review analysis, authoritative replacement files, and local verification instructions.
Drop	Scope	Completion outcome
1. FO Administration & Settings	

- Retire five FO toggles, correct startup settings persistence, enforce explicit permissions, remove misleading Admin Console status	

- Existing administrative controls are safe and consistent

2. Installation & Bootstrap Security	

- One-time installer, secure email delivery, account establishment, persistent installation state	

- Root can be established securely without permanent bootstrap email configuration

3. Root Continuity & Recovery	

- Incumbent/co-Root governance, three designated successors, succession ceremonies, protected Board + Engineering recovery	

- Administrative continuity is governed and auditable

4. Platform Terminology & Angular	

- Terminology persistence/API, Admin Console management, merchant-facing adoption, SSR integration	

- “What's New” becomes centrally configurable


The important distinction: each drop is a complete vertical implementation, not merely a database layer followed by services in another drop.
I'll author each from the existing repository contracts, using SE's work as input rather than as the authoritative foundation.
We should begin with Drop 1. It addresses existing settings and permission defects without requiring us to redesign Root identity first.
And we retain our agreed boundary throughout: source review and code authoring only. You handle local execution and testing.
You've given me a practical way through the volume. Thank you.



# Drop 1 — FO Administration & Settings

The original backend confirms two defects that Drop 1 must correct:

1. Startup calls PlatformSetting.Ensure(...) for canonical defaults. That operation must be replaced with insert-if-absent behavior so existing administrator settings survive restarts.

2. future_offering_enabled is both a seeded setting and an Admin Console status field. It must be retired as an administrative control, together with the four related FO availability toggles.
Drop 1 will retain legitimate merchant debit-card settings and other bounded configuration. Retiring FO availability controls must not accidentally remove unrelated merchant requirements.
Engineering decision

>There is one important dependency: retiring the FO toggles must be enforced at the mutation boundary, not merely by removing their defaults and hiding them in Angular. Otherwise, an existing setting row or an administrative API request could still change one.

# I suggest we set aside retiring the FO toggles for noe, set them to off, and avoid the rework. If possible, we should simply make it impossible to change by disabling the administrative switch, and marking it "Not Allowed"

Only thing that changes about FO now is that the administrative switch is disabled  (false) and Admin is not allowed to make any changes. That way, it stays in Engineering control.

Agreed. That's the complete FO policy adjustment for Drop 1.

- **Administrative switch:** `false` (Off).

- **Administrative permission:** `Not Allowed` — Admin cannot modify it.

- **Authority:** Engineering retains control of FO availability.

- **Business functionality:** FO creation, management, discovery, and consumer engagement remain operational.

- **Architecture:** Existing SPINE policy remains intact. The proposed replacement stays DEFERRED.

The backend must enforce `Not Allowed`; disabling the control in Angular alone is insufficient.

No policy migration. No application redesign.

We now have a precise, bounded requirement for Drop 1.


We should also distinguish **scheduled maintenance** from **active maintenance**.

# 1. Scheduled Maintenance — Announcement

The platform remains fully operational.

- Root Super Admin schedules the maintenance window and publishes an announcement.

- A prominent, dismissible announcement bar appears above the site header, across merchant and consumer experiences.

- The announcement includes the planned start time, expected duration, and a short explanation.

- Root can update or cancel the announcement.


# 2. Active Maintenance — Platform Offline

Root Super Admin activates maintenance, either at the scheduled time or manually.

- Merchant and consumer operations become temporarily unavailable.

- Visitors see a dedicated, branded maintenance page.

- Root retains secure administrative access to manage maintenance and restore service.

- Engineering retains emergency recovery capability.

- Restoration requires an explicit authorized action; the platform does not automatically reopen simply because the scheduled window ends.

> Scheduled ≠ Active. Publishing an announcement must never, by itself, disable the platform.
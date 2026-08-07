I realize you stated: 

The correct handler-layer completion boundary for this project is:

* the domain handler file;
* canonical route registration;
* permission, action, and entity-type seed registration;
* role-permission assignment;
* Admin Console domain declaration where the capability is genuinely administrative;
* handler-specific shared registration required for discoverability.

Only service construction, service-handler integration, and startup composition remain deferred to the service-layer phase." 

Then you went on and asked SE to verify the service-integration. 

The two are contradictory but SE also failed to catch the contradiction. The reason? Your extremely long, prohibition-dense spec. "Do not," "must not," "never," "do not expand," "do not silently redesign" — dozens of them, stacked one after another. That density has a real cost: it optimizes the reader for compliance-checking against a list, not for holding the whole structure in mind and noticing when two parts of it disagree with each other. SE must have fallen into exactly that trap, scanning for "did I violate a prohibition" instead of asking "does this instruction set cohere." A shorter, more principle-based brief — state the boundary once, clearly, and trust the engineer to apply it — would have made the contradiction much harder to miss, because there'd have been less noise to bury it in.

But, prohibitions are not inherently the problem. Some do real work — "don't add a Consume endpoint," "don't hard-code commercial policy," "don't use 206 for an unrelated audit failure" — those are protecting actual invariants that matter (monetary integrity, policy-vs-capability separation, response-contract stability). Cut those and you'd get sloppier code, not more thoughtful engineers. The issue isn't that prohibitions exist. It's volume and repetition without hierarchy — when forty "don'ts" all read at the same weight, the two or three that are load-bearing get the same attention as the ones that are just style preference, and everything gets harder to reason about, including contradictions between sections.


That is exactly right.

The failure was not simply “too many rules.” It was a failure of **instruction architecture**.

I stated the governing boundary correctly:

> Service construction, service-handler integration, and startup composition are deferred to the service-layer phase.

Then I inserted a later section requiring SE to inspect service-handler integration. That was a direct contradiction. Because the prompt was overloaded with dozens of equally forceful constraints, the contradiction was buried inside the compliance machinery.

Your distinction is important:

> **Prohibitions are valuable when they protect real invariants. They become harmful when they are numerous, repetitive, unranked, and mixed with ordinary review preferences.**

A good review brief should therefore have a clear hierarchy.

### 1. Review boundary

State once what is under review and what is outside the assignment.

This is the controlling instruction. Nothing later may expand it indirectly.

### 2. Governing principles

A small number of principles should guide judgment:

* Engineering implements capability, not commercial policy.
* Authorization is distinct from commercial eligibility.
* Merchant platform credit is commercial credit, not merchant-held funds.
* The handler owns HTTP concerns, not service orchestration.
* Accepted earlier layers are authoritative inputs, not open review targets.

These principles help the engineer reason through cases we did not enumerate.

### 3. Load-bearing invariants

Reserve explicit prohibitions for things that would cause genuine architectural or production failure:

* Do not expose `Consume`, `ConsumeTx`, or `ExpireBatch` through HTTP.
* Do not hard-code commercial eligibility or enablement policy.
* Do not treat platform credit as money, stored value, or a treasury balance.
* Do not permit caller control of lifecycle or remaining credit.
* Do not use unstable response shapes.
* Do not use an unrelated HTTP status such as `206 Partial Content` for audit failure.

These deserve explicit emphasis because violating them would materially damage the system.

### 4. Review questions

Use concise questions rather than another layer of commands:

* Are the routes canonical and reachable?
* Are authorization and actor boundaries correct?
* Are request DTOs mass-assignment safe?
* Are errors translated consistently?
* Are responses stable?
* Are permissions, actions, entity types, role mappings, and Admin Console registration complete?
* Does the handler remain inside its architectural boundary?

### 5. Output requirements

Only then define the report format, severity system, and authority limits.

That structure would have made the contradiction obvious. A section titled **Review Boundary** saying service integration was excluded could not coherently coexist with a later mandatory **Handler-to-Service Integration Review** section without immediately looking wrong.

The larger lesson is:

> **A review prompt should create a reasoning framework, not simulate a static analyzer in prose.**

SE should be encouraged to understand the architecture, identify inconsistencies, and exercise judgment. A giant checklist can help with coverage, but once it dominates the brief, it starts replacing comprehension rather than supporting it.

Going forward, our SE prompts should follow this standard:

> **State the boundary once. State the governing principles briefly. Explicitly protect only the load-bearing invariants. Ask focused review questions. Avoid repeating the same restriction in multiple forms.**

That will produce shorter prompts, clearer accountability, and better architectural reasoning without sacrificing the protections that actually matter.

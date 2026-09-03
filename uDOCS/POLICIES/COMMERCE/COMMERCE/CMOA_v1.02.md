# Commerce Model & Offer Architecture

## CMOA v1.02

### 1. Strategic Purpose

The Commerce Model & Offer Architecture defines Sagrenti’s permanent commerce structure.

Sagrenti has one business.

The public product is Offers.

Offers are the consumer-facing expression of Sagrenti’s commerce system. Sagrenti does not present separate consumer products for deals, launch intelligence, affiliate campaigns, merchant campaigns, waitlists, preorders, reservations, or anticipation analytics. Those are internal commercial, merchant-side, or engagement structures.

To consumers, Sagrenti presents Offers.

Offers are of two kinds:

1. Deal
2. Trend

This distinction is authoritative.

Deals represent Present Commerce.

Trends represent Future Commerce.

The merchant-side source of Future Commerce is the Future Offering.

A Future Offering is a merchant-owned product or service that is not yet generally available.

A Future Offering intentionally includes both products and services, including but not limited to consumer products, software, events, hospitality, travel, financial services, and other future commercial offerings.

Launch Intelligence operates on Future Offerings.

Trend offers are the consumer presentation of Future Offerings.

When a Future Offering reaches general availability, it graduates from a Trend to a Deal.

The commerce chain is therefore:

Future Offering
→ Trend
→ Deal, when generally available

This document is the authoritative reference for database schema, APIs, backend services, frontend UI, analytics, billing, commerce routes, Merchant Center, and long-term competitive strategy.

---

### 2. The Single Business Doctrine

Sagrenti is not multiple disconnected businesses.

It is not separately:

* a deals app;
* an affiliate site;
* a merchant campaign platform;
* a launch intelligence product;
* a wishlist tool;
* a waitlist tool;
* a preorder directory;
* a reservation system;
* a traffic broker.

Sagrenti is one commerce intelligence business expressed through Offers.

The consumer sees Offers. The merchant brings Future Offerings. Sagrenti transforms those Future Offerings into consumer-facing commerce objects.

The governing structure is:

Future Offering
→ Offer

The public Offer is either:

* Deal, for Present Commerce;
* Trend, for Future Commerce.

This doctrine prevents product drift.

Affiliate Commerce, Launch Campaigns, and Merchant Launch Intelligence are not separate public products. They are origin systems, monetization paths, and merchant-side commercial structures that feed the Offer architecture.

---

### 3. Future Offering Doctrine

A Future Offering is the merchant-side future-commerce object.

It is a merchant-owned product or service that is not yet generally available.

Future Offering intentionally includes both products and services, including but not limited to consumer products, software, events, hospitality, travel, financial services, and other future commercial offerings.

A Future Offering may be a product, service, launch, opening, release, programme, experience, availability event, reservation opportunity, beta opportunity, preorder opportunity, invitation opportunity, or other future commercial initiative.

There is no Future Commerce business without a Future Offering.

The Future Offering is not itself the consumer product surface. It is the merchant-side source from which Sagrenti creates the appropriate public Trend.

If the merchant object is not yet generally available, it is expressed as a Trend.

When the Future Offering becomes generally available, the Trend may graduate into a Deal.

The architecture is:

Future Offering
→ Trend, when future-facing

Trend
→ Deal, when generally available

This preserves the distinction between what the merchant owns, what the consumer sees, and what Sagrenti measures.

---

### 4. Future Offering Categories

Sagrenti may support Future Offerings across any lawful commercial category where anticipation, availability, purchase intent, reservation intent, or launch interest can be meaningfully measured.

The following categories are authoritative examples of supported Future Offerings.

#### 4.1 Consumer Products

* Smartphone, including examples such as iPhone 20
* Startup gadget
* Smart glasses
* Laptop
* Tablet
* Smartwatch
* Audio device, including headphones, earbuds, and speakers
* Camera
* Gaming console
* Home appliance

#### 4.2 Fashion & Luxury

* Luxury handbag collection
* Limited sneaker release
* Fashion collection
* Jewelry collection
* Watch collection
* Cosmetics or beauty collection

#### 4.3 Automotive & Mobility

* Electric vehicle
* Motorcycle
* Bicycle
* Boat

#### 4.4 Technology & Software

* SaaS platform
* Mobile application
* AI service
* Software product
* Hardware device

#### 4.5 Creator & Media

* Creator merchandise
* Creator course
* Book
* Music album
* Film or documentary
* Video game

#### 4.6 Hospitality & Travel

* Hotel opening
* Resort opening
* Restaurant opening
* Café opening
* Cruise
* Travel experience

#### 4.7 Events

* Music festival
* Conference
* Trade show
* Sporting event
* Exhibition

#### 4.8 Real Estate

* Residential development
* Commercial development
* Mixed-use development

#### 4.9 Health & Wellness

* Medical device
* Healthcare service
* Fitness programme
* Wellness product

#### 4.10 Financial Services

* Banking product
* Insurance product
* Investment product
* Fintech service

#### 4.11 Other Services

* Subscription service
* Membership programme
* Professional service
* Educational programme

This list is not a separate product taxonomy for consumers. It is the merchant-side Future Offering taxonomy used to classify what a merchant is bringing into Sagrenti before general availability.

---

### 5. Offer Taxonomy

Sagrenti Offers have two public kinds.

#### 5.1 Deal

A Deal is a generally available commercial offer.

A Deal belongs to Present Commerce.

A Deal may originate from:

* Affiliate Commerce;
* Launch Campaigns;
* a merchant product or service that is available now;
* a Trend that has graduated into general availability.

Consumers do not need to know the Deal’s source.

A Deal may route the consumer to a merchant checkout, merchant campaign destination, affiliate link, claim page, purchase page, reservation page, booking page, or other present-commerce destination.

Wishlist belongs only to Deals.

Saving a Deal means the consumer is saving a generally available offer for later purchase or action.

#### 5.2 Trend

A Trend is a future-commerce offer.

A Trend represents consumer-facing visibility into an upcoming Future Offering.

A Trend may represent an upcoming product, service, launch, release, drop, opening, availability event, programme, experience, reservation opportunity, beta opportunity, preorder opportunity, invitation opportunity, or merchant commercial initiative.

A Trend originates from Merchant Launch Intelligence.

Launch Intelligence is not a separate consumer product. It is presented to consumers as a Trend offer.

A Trend allows Sagrenti to measure anticipation before the Future Offering becomes generally available.

Every Trend is inherently watchable.

---

### 6. Present Commerce

Present Commerce concerns offers that are generally available now.

The consumer may act on them in a purchase-oriented way.

Present Commerce includes:

* Deals;
* Affiliate Commerce;
* Launch Campaigns;
* Wishlist behavior;
* outbound commerce routing;
* present-commerce performance analytics;
* Campaign Performance Fees.

A Deal is the public consumer object of Present Commerce.

Deals may be created from affiliate sources, merchant campaign sources, generally available merchant products or services, or graduated Trends.

The origin affects routing, attribution, analytics, and billing, but it should not fragment the consumer experience.

Consumers should not be required to understand whether a Deal came from an affiliate network, a merchant Launch Campaign, or a graduated Trend.

For the consumer, it is simply a Deal.

---

### 7. Future Commerce

Future Commerce concerns Future Offerings that are not yet generally available as ordinary commercial Deals.

The consumer cannot simply treat the offer as a normal present-commerce purchase opportunity.

Future Commerce includes:

* Future Offerings;
* Trends;
* watch behavior;
* waitlist intent;
* early-access interest;
* preorder interest;
* beta interest;
* reservation intent;
* invitation interest;
* repeat anticipation engagement;
* first-party consumer intent data;
* Launch Intelligence Fees;
* merchant anticipation analytics.

A Trend is the public consumer object of Future Commerce.

The Future Offering is the merchant-side source.

Trends are not Deals. Trends do not become Deals directly through routing or affiliate mechanics.

Sagrenti must preserve the lifecycle distinction:

Future Offering
→ Future Commerce
→ Trend

When the Future Offering becomes generally available:

Trend graduates into:

Present Commerce
→ Deal

After graduation, the Deal may participate in:

* Launch Campaigns, preferred;
* Affiliate Commerce, where appropriate.

This lifecycle is authoritative.

---

### 8. Consumer Engagement Options

Consumer Engagement Options are the ways consumers express anticipation toward a Trend representing a Future Offering.

Consumer Engagement Options are not separate consumer products. They are engagement paths attached to Trends.

Examples include:

* Watch
* Waitlist
* Early Access
* Preorder
* Beta
* Reservation
* Invitation

Every Trend is inherently watchable.

Every Future Offering may expose one or more Consumer Engagement Options.

The merchant determines which engagement options are available for each Future Offering.

Different Future Offerings may expose different engagement options.

Consumer engagement generates first-party anticipation intelligence owned by Sagrenti.

Notifications naturally follow consumer engagement with Future Offerings.

Future Offerings should remain first-party within Sagrenti whenever practical, with Merchant Center serving as the primary interface for merchants to view anticipation intelligence and engagement metrics.

#### 8.1 Illustrative Engagement Options

| Future Offering            | Consumer Engagement Options                  |
| -------------------------- | -------------------------------------------- |
| Smartphone launch          | Watch, Waitlist, Early Access, Preorder      |
| Startup gadget             | Watch, Waitlist, Beta, Early Access          |
| Luxury handbag collection  | Watch, Reservation, Early Access, Invitation |
| Music festival             | Watch, Early Access, Preorder                |
| Hotel opening              | Watch, Reservation                           |
| Restaurant opening         | Watch, Reservation, Invitation               |
| SaaS platform              | Watch, Beta, Waitlist, Early Access          |
| Creator course             | Watch, Waitlist, Early Access, Preorder      |
| Electric vehicle launch    | Watch, Reservation, Early Access             |
| Financial services product | Watch, Waitlist, Invitation                  |

This table is illustrative, not exhaustive.

The governing principle is that every Trend can be watched, while additional engagement options depend on the nature of the Future Offering and the merchant’s chosen launch strategy.

---

### 9. Launch Intelligence Doctrine

Merchant Launch Intelligence begins with a Future Offering.

The merchant brings a future product, service, launch, opening, release, programme, experience, reservation opportunity, beta opportunity, preorder opportunity, invitation opportunity, or commercial availability event into Sagrenti.

Sagrenti expresses that Future Offering to consumers as a Trend.

Merchant Launch Intelligence is not a separate consumer-facing product.

Consumers should not see “Launch Intelligence” as a product category. They should see Trends.

Sagrenti owns the consumer interaction layer around Trends. This includes:

* Watch;
* Waitlist;
* Early Access;
* Preorder interest;
* Beta interest;
* Reservation intent;
* Invitation interest;
* Anticipation intelligence.

Merchants purchase the intelligence generated from these interactions.

They are not primarily purchasing anonymous outbound traffic. They are purchasing measurable anticipation from Sagrenti-owned first-party behavior.

This is the central difference between Sagrenti and ordinary affiliate or campaign platforms.

---

### 10. Trend-to-Deal Graduation

Launch Intelligence must never directly become a Deal.

A Future Offering is first represented as a Trend when it is future-facing.

A Trend may graduate into a Deal only when the underlying Future Offering becomes generally available.

The proper lifecycle is:

1. Merchant submits or activates a Future Offering.
2. Sagrenti presents the Future Offering to consumers as a Trend.
3. Merchant selects the available Consumer Engagement Options.
4. Consumers watch, join, request, reserve, preorder, accept invitation, or otherwise express interest.
5. Sagrenti measures anticipation.
6. Merchant receives launch intelligence through Merchant Center.
7. Future Offering becomes generally available.
8. Trend graduates into a Deal.
9. Deal participates in Launch Campaigns or Affiliate Commerce.

This preserves semantic clarity.

A Future Offering is what the merchant owns.

A Trend measures anticipation.

A Deal supports present-commerce action.

Confusing the three would damage analytics, billing, user expectations, merchant reporting, and strategic moat.

---

### 11. Consumer Experience

The consumer experience must be simple.

Consumers browse Offers.

An Offer is either a Deal or a Trend.

Consumers do not need to understand the internal Future Offering object.

#### 11.1 Deal Consumer Experience

For Deals, consumers may:

* view the Deal;
* compare the Deal;
* save the Deal to Wishlist;
* act on the Deal;
* route outbound to a merchant or affiliate destination;
* receive relevant present-commerce notifications.

Wishlist applies only to Deals.

Wishlist means the consumer is saving an available commercial offer for later purchase or action.

Wishlist must not be used for Trends because a Trend is not yet an ordinary available purchase opportunity.

#### 11.2 Trend Consumer Experience

For Trends, consumers may:

* watch the Trend;
* join a waitlist;
* request early access;
* express preorder interest;
* request beta access;
* make a reservation;
* respond to an invitation;
* receive engagement-based notifications.

Every Trend is inherently watchable.

Watching can occur independently.

Joining a waitlist, requesting early access, expressing preorder interest, requesting beta access, making a reservation, or responding to an invitation all imply an ongoing watch relationship.

Notifications follow consumer engagement with Trends. Notifications are not a separate primary intent.

A consumer does not primarily “subscribe to notifications.” The consumer watches, joins, requests, reserves, preorders, or expresses interest. Notification rights follow from that engagement.

#### 11.3 First-Party Trend Principle

Consumers should not ordinarily leave Sagrenti during Trend interactions.

Trend engagement should remain first-party whenever possible.

This allows Sagrenti to own the anticipation signal rather than surrendering the most valuable part of the interaction to a merchant site.

Outbound routing is normal for Deals. It is not the default for Trends.

---

### 12. Merchant Experience

Merchants interact with Sagrenti through Merchant Center.

Merchant Center must reflect the commerce model.

A merchant should understand that Sagrenti supports:

* Future Offerings as the merchant-side future-commerce source;
* available offers through Deals and Launch Campaigns;
* upcoming launches through Trends and Front Row;
* future-commerce intelligence through first-party anticipation data.

#### 12.1 Future Offering Experience

The merchant begins with a Future Offering.

The Future Offering may be:

* upcoming;
* accepting watch interest;
* accepting waitlist interest;
* accepting early-access interest;
* accepting preorder interest;
* accepting beta interest;
* accepting reservation intent;
* accepting invitation interest;
* preparing for launch;
* preparing for opening;
* preparing for release;
* measuring demand.

Merchant Center should treat the Future Offering as the merchant-owned commercial unit from which Sagrenti creates a Trend.

The merchant determines which Consumer Engagement Options are available for each Future Offering.

#### 12.2 Merchant Experience for Deals

For Deals, merchants care about present-commerce performance.

Merchant Center may show:

* Deal impressions;
* qualified clicks;
* campaign engagement;
* Wishlist saves;
* routing events;
* conversion-related campaign events;
* Campaign Performance Fee events.

The merchant objective is action on an available offer.

#### 12.3 Merchant Experience for Trends

For Trends, merchants care about anticipation.

Merchant Center may show:

* Trend views;
* watch count;
* waitlist joins;
* early-access requests;
* preorder interest;
* beta requests;
* reservation interest;
* invitation responses;
* repeat engagement;
* consumer segments;
* anticipation velocity;
* launch readiness signals;
* Launch Intelligence Fee events.

The merchant objective is not merely traffic. It is understanding demand before availability.

#### 12.4 Merchant Value Proposition

Merchants purchase anticipation intelligence rather than anonymous outbound traffic.

This means Sagrenti must present merchant dashboards around first-party signals:

* who is watching;
* how interest is growing;
* which Future Offerings are gaining momentum;
* which consumers are showing deeper intent;
* which engagement options are producing the strongest signal;
* which launch messages produce engagement;
* when a Trend is ready to graduate into a Deal.

---

### 13. Analytics Implications

Analytics must preserve the Future Offering, Deal, and Trend distinction.

#### 13.1 Future Offering Analytics

Future Offering analytics answer:

* What future product or service has the merchant brought into Sagrenti?
* Which Consumer Engagement Options are enabled?
* Is the Future Offering represented as a Trend?
* What consumer intent has accumulated around it?
* Has it graduated from Trend to Deal?
* Did anticipation predict present-commerce performance?

Future Offering analytics form the bridge between merchant-side commercial planning and consumer-side Trend behavior.

#### 13.2 Deal Analytics

Deal analytics measure present-commerce behavior.

Relevant signals include:

* views;
* clicks;
* Wishlist saves;
* outbound routing;
* campaign actions;
* affiliate attribution;
* merchant-direct campaign events;
* conversions where available;
* Campaign Performance Fee calculations.

Deal analytics answer:

* Is this available offer attracting purchase intent?
* Are consumers saving it for later action?
* Are consumers routing to the merchant?
* Is the campaign producing measurable outcomes?

#### 13.3 Trend Analytics

Trend analytics measure future-commerce anticipation.

Relevant signals include:

* Trend discovery;
* watches;
* waitlist joins;
* early-access requests;
* preorder interest;
* beta requests;
* reservations;
* invitation responses;
* repeat visits;
* engagement depth;
* anticipation growth;
* launch readiness;
* audience quality.

Trend analytics answer:

* Is this Future Offering attracting interest?
* Which Consumer Engagement Options are producing meaningful anticipation?
* Are consumers willing to maintain an ongoing relationship with the launch?
* Is anticipation growing?
* Which signals suggest commercial readiness?
* What intelligence can Sagrenti provide the merchant before launch?

#### 13.4 Analytics Boundary

Wishlist must not be used as a Trend metric.

Trend watch must not be treated as Wishlist.

Deal outbound routing must not be treated as launch anticipation.

Trend engagement must not be treated as affiliate traffic.

Consumer Engagement Options must not be treated as separate public products.

Future Offering classification must not be confused with public Offer type.

These boundaries preserve measurement integrity.

---

### 14. Billing Implications

Billing must follow commerce meaning.

#### 14.1 Future Offering Billing Context

The Future Offering is the commercial context for future-commerce merchant billing.

Billing may attach to:

* merchant plan access;
* setup or activation;
* future-commerce launch intelligence;
* qualified engagement;
* watch activity;
* waitlist activity;
* early-access activity;
* preorder interest;
* beta interest;
* reservation interest;
* invitation response;
* other policy-defined billable outcomes.

The Future Offering gives the billing event its commercial context.

#### 14.2 Deal Billing

Deals may produce billing through:

* affiliate/referral commissions;
* Launch Campaign subscription;
* Campaign Performance Fees;
* merchant-direct campaign charges.

Deal billing belongs to Present Commerce.

Campaign Performance Fees are tied to measurable present-commerce outcomes.

#### 14.3 Trend Billing

Trends may produce billing through:

* Front Row subscription;
* Launch Intelligence Fees;
* launch analytics access;
* future-commerce merchant intelligence.

Trend billing belongs to Future Commerce.

Launch Intelligence Fees are tied to measurable anticipation outcomes.

#### 14.4 Fee Integrity

Campaign Performance Fees and Launch Intelligence Fees must remain separate.

A Campaign Performance Fee validates present-commerce performance.

A Launch Intelligence Fee validates merchant willingness to pay for anticipation intelligence.

This separation is central to Sagrenti’s business model.

---

### 15. Commerce Routes

Commerce routes must reflect the lifecycle of the Future Offering and Offer.

#### 15.1 Deal Routing

Deal routing is the normal outbound commerce path.

A Deal may route to:

* merchant checkout;
* merchant campaign page;
* affiliate destination;
* reservation flow;
* claim flow;
* purchase flow;
* booking flow;
* other present-commerce endpoint.

Deal routing supports present-commerce conversion and attribution.

#### 15.2 Trend Routing

Trend interactions should remain first-party whenever possible.

A Trend should normally support:

* watch;
* waitlist;
* early-access request;
* preorder interest;
* beta request;
* reservation interest;
* invitation response;
* launch engagement;
* internal anticipation capture.

Trend routing to merchants should be controlled and secondary.

Sagrenti should not surrender its most valuable anticipation signal prematurely.

#### 15.3 Graduation Routing

When a Trend graduates into a Deal, outbound commerce routing becomes appropriate.

At that point, the Future Offering is generally available, and the Deal may participate in Launch Campaigns or Affiliate Commerce.

This transition must be explicit and analytically visible.

---

### 16. Database Architecture Implications

The database architecture must encode the commerce model without ambiguity.

#### 16.1 Future Offerings as Merchant-Side Root

Future Offerings should be treated as the merchant-side future-commerce root.

A Future Offering represents the product or service the merchant is bringing into Sagrenti before general availability.

It may give rise to:

* a Trend;
* a Trend that later graduates into a Deal.

The Future Offering preserves merchant-side continuity across lifecycle changes.

#### 16.2 Offers as the Public Root

Offers should be the public-facing root object.

Each Offer must be classified as:

* Deal;
* Trend.

This classification governs valid behavior, routing, analytics, and billing.

#### 16.3 Source vs Public Type

Offer source and offer type are not the same.

Public type:

* Deal;
* Trend.

Source or origin may include:

* Affiliate Commerce;
* Launch Campaign;
* Merchant Launch Intelligence;
* Future Offering.

Consumers see type. Internal systems track source.

This prevents consumer-facing fragmentation while preserving operational truth.

#### 16.4 Deal-Specific Relationships

Deal-related structures may include:

* Wishlist;
* affiliate routing;
* merchant campaign routing;
* present-commerce performance events;
* campaign billing events.

Wishlist must attach only to Deals.

#### 16.5 Trend-Specific Relationships

Trend-related structures may include:

* watches;
* waitlist entries;
* early-access requests;
* preorder interest;
* beta requests;
* reservations;
* invitation responses;
* anticipation events;
* launch intelligence billing events.

Every waitlist, early-access, preorder-interest, beta, reservation, or invitation action implies an ongoing watch relationship.

#### 16.6 Consumer Engagement Option Relationships

Consumer Engagement Options should be represented in a way that preserves merchant control and analytics integrity.

The system must be able to know:

* which Consumer Engagement Options are enabled for a Future Offering;
* which options were visible to consumers;
* which options consumers used;
* when an option was enabled, disabled, or changed;
* whether engagement occurred before or after a Future Offering graduated;
* which engagement options produced measurable anticipation.

This allows Sagrenti to analyze not only whether consumers cared about a Future Offering, but how they chose to express that anticipation.

#### 16.7 Lifecycle Linkage

A Trend that graduates into a Deal should preserve lineage.

The system must be able to know:

* which Future Offering produced the Trend;
* which Trend preceded the Deal;
* what anticipation existed before general availability;
* which Consumer Engagement Options generated that anticipation;
* how Trend engagement translated into Deal performance;
* which merchant intelligence preceded launch;
* whether future-commerce signals predicted present-commerce outcomes.

This lineage is a strategic asset.

---

### 17. API Implications

APIs must respect Future Offering and Offer type boundaries.

Merchant APIs may expose Future Offerings as merchant-owned future-commerce objects.

Consumer APIs should expose Offers as Deals or Trends.

Deal APIs should support present-commerce behavior:

* Deal viewing;
* Wishlist;
* outbound routing;
* campaign action;
* present-commerce analytics.

Trend APIs should support future-commerce behavior:

* watch;
* waitlist;
* early access;
* preorder interest;
* beta request;
* reservation interest;
* invitation response;
* anticipation analytics;
* graduation readiness.

Merchant APIs should allow merchants to configure Consumer Engagement Options for each Future Offering.

Consumer APIs should expose only the engagement options available for the relevant Trend.

APIs must not expose Launch Intelligence as a separate consumer product.

Launch Intelligence belongs to merchant-side and analytics systems. The consumer-facing object is the Trend.

---

### 18. Frontend UI Implications

The frontend must make the Offer model intuitive.

Consumers should understand the difference between:

* Deals: available now;
* Trends: worth watching before availability.

The UI should not require consumers to understand Future Offerings as an internal merchant-side concept.

The UI should not overload Wishlist.

Wishlist is for Deals.

Trend engagement should use language appropriate to future-commerce intent:

* Watch this Trend;
* Join Waitlist;
* Get Early Access;
* Preorder;
* Request Beta;
* Reserve;
* Request Invitation.

The UI should avoid asking consumers to manage abstract notification subscriptions as the primary action. Notifications should follow from engagement.

Merchant UI should distinguish:

* Future Offerings;
* Consumer Engagement Options;
* Deal performance;
* Trend anticipation;
* Launch Campaigns;
* Front Row intelligence.

---

### 19. Merchant Center Implications

Merchant Center must be organized around the commerce model.

The merchant should see that Sagrenti supports:

* Future Offerings as the merchant-owned future-commerce source;
* Present Commerce through Deals and Launch Campaigns;
* Future Commerce through Trends and Front Row;
* Consumer Engagement Options configured per Future Offering;
* billing through access, performance, and intelligence fees;
* analytics through first-party engagement signals.

Merchant Center dashboards must not collapse Trend metrics into Deal metrics.

A watch is not a Wishlist save.

A waitlist join is not an outbound click.

A preorder-interest signal is not an affiliate conversion.

A reservation signal is not ordinary checkout behavior.

A beta request is not a Deal action.

An invitation response is not a generic notification subscription.

A Future Offering is not automatically a Deal.

Each signal has different commercial meaning.

Merchant Center should be the primary interface for merchants to view anticipation intelligence and engagement metrics.

Future Offerings should remain first-party within Sagrenti whenever practical.

---

### 20. Competitive Moat

Sagrenti’s long-term moat depends on first-party anticipation intelligence around Future Offerings.

Affiliate commerce is useful but not defensible by itself.

Deal aggregation is useful but not defensible by itself.

Outbound traffic is useful but not defensible by itself.

The defensible layer is Sagrenti’s ability to observe, measure, and interpret consumer anticipation before general availability.

Future Offerings give Sagrenti the upstream merchant-owned commercial object.

Trends give Sagrenti the consumer-facing future-commerce object.

Consumer Engagement Options give Sagrenti the structured first-party signals of anticipation.

Deals give Sagrenti the present-commerce conversion object.

Together, they create a proprietary lifecycle:

Future Offering
→ Trend anticipation
→ Deal conversion

By keeping Trend interactions first-party, Sagrenti owns the anticipation graph:

* what consumers watch;
* what they join;
* what they request;
* what they reserve;
* what they express preorder interest in;
* what they seek beta access for;
* what they respond to by invitation;
* what they return to;
* which Future Offerings gain momentum;
* which launches convert anticipation into commerce.

This data improves merchant value, consumer relevance, launch prediction, and platform defensibility.

The Future Offering-to-Trend-to-Deal lifecycle creates a proprietary bridge from future intent to present commerce.

That bridge is Sagrenti’s strategic advantage.

---

### 21. Permanent Architecture Doctrine

The following doctrine is authoritative.

Sagrenti has one business.

The public product is Offers.

The merchant-side future-commerce source object is the Future Offering.

A Future Offering is a merchant-owned product or service that is not yet generally available.

Future Offering intentionally includes both products and services, including but not limited to consumer products, software, events, hospitality, travel, financial services, and other future commercial offerings.

Offers are of two kinds:

* Deal;
* Trend.

Deals represent Present Commerce.

Deals may originate from Affiliate Commerce, Launch Campaigns, generally available merchant products or services, or graduated Trends.

Consumers do not need to know the source.

Trends represent Future Commerce.

Trends originate from Future Offerings through Merchant Launch Intelligence.

Launch Intelligence operates on Future Offerings.

Launch Intelligence is not a separate consumer product. It is presented as a Trend offer.

Launch Intelligence must never directly become a Deal.

The authoritative lifecycle is:

Future Offering
→ Trend
→ Deal, when generally available

Wishlist belongs only to Deals.

Every Trend is inherently watchable.

Consumer Engagement Options are the ways consumers express anticipation toward a Trend representing a Future Offering.

Consumer Engagement Options include examples such as:

* Watch;
* Waitlist;
* Early Access;
* Preorder;
* Beta;
* Reservation;
* Invitation.

Every Future Offering may expose one or more Consumer Engagement Options.

The merchant determines which engagement options are available for each Future Offering.

Different Future Offerings may expose different engagement options.

Consumer engagement generates first-party anticipation intelligence owned by Sagrenti.

Notifications follow engagement. They are not the primary intent.

Deal routing is the normal outbound commerce path.

Trend interactions remain first-party whenever possible.

Future Offerings should remain first-party within Sagrenti whenever practical.

Merchant Center is the primary interface for merchants to view anticipation intelligence and engagement metrics.

Sagrenti owns Watch, Waitlist, Early Access, Preorder interest, Beta interest, Reservation intent, Invitation interest, and Anticipation Intelligence.

Merchants purchase anticipation intelligence rather than anonymous outbound traffic.

Database schema, APIs, backend services, frontend UI, analytics, billing, commerce routes, and Merchant Center must follow this architecture.

This is the governing commerce model of CMOA v1.02.

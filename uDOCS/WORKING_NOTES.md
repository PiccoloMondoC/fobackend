## To update code on Github:
git add -A
git commit -m "write merchant_program_subscription_events.go for all 3 layers: data, handler, and services"
git push

| data layer | handler layer | services layer |

### To Test
gofmt -w ./internal/data/errors.go

### Vertical Build Strategy
Our strategy is  to build vertically across each of the 27 files listed below, not horizontally. Completing one file across data → handlers → services → startup → reviews means each slice reaches a genuine state of completion before we move on.

By the time we finish the 27th Future Offering file, we won't have 27 partially built components—we'll have a coherent, production-grade subsystem awaiting integration testing.


### Document Context map
[SSD]
Why does Sagrenti exist?
        │
        ▼
SPA
What platform must exist to fulfill that strategy?
        │
        ├───────────────┐
        ▼               ▼
SEA                    FCDS
How do we execute?     How do we compete and defend?
        │               │
        └───────┬───────┘
                ▼
        Commerce Architecture
                │
        ┌───────┼────────┐
        ▼       ▼        ▼
      CMOA     FCP      MCBS
   Objects &   Future   Revenue
relationships Commerce model
        │       │        │
        └───────┼────────┘
                ▼
              BEG
 How engineers must implement all of it
                │
                ▼
 Future Offering Experience Architecture
 Information architecture, journeys,
 page hierarchy and interaction rules
                │
                ▼
       Angular Frontend Architecture
 Routes, components, services, state,
 APIs, permissions and design system
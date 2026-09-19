# Network workspace

The network groups channels by their explicit creator assignment by default. Expand a creator to inspect its accounts separately, or turn grouping off. Links within a creator are retained in the underlying channel data and appear when expanded; cross-creator links combine their stored evidence and observation counts.

Search matches names, platform identities, profile URLs, grouped accounts, and attached vault titles. Graph search includes direct neighbors. Select a node to focus its neighborhood, then expand up to five steps. The default graph shows connected accounts and known multi-account creators; the list view and search also include isolated accounts. The existing creator-bundle filter remains available.

Select a connection to read its stored evidence and open the supporting video when available. Harvested edge weights are observation counts, not independent corroboration or proof of a personal relationship. The graph uses the existing strongest-500-edge query.

Vault (encyclopedia) wikilinks are a fourth edge kind, drawn in green and labeled `wiki`. They are memory, not harvested observations: a `[[creator/devan-costa]]` on Ben's page is a vault cross-reference. Creators who have vault pages but no archived channels still appear when a wikilink reaches them. The inspector lists attached vault pages and links to the encyclopedia. The Vault checkbox hides those edges without removing harvested links.

The inspector provides archive links, vault pages, source profiles, follow setup, creator workspace navigation, and an assistant prompt. Assistant actions prefill a prompt for review; they do not send it automatically.

## Identity tools and X

Select known accounts and assign them to an existing or new creator. Add X profiles or former handles with comma-separated handles or x.com/twitter.com profile URLs. Inputs are normalized case-insensitively; post URLs and navigation URLs are rejected. Existing account identities and exact profile URLs are reused. Display-name similarity does not automatically merge accounts.

Grouping is transactional and will reject an account already assigned to a different creator. Ungroup it explicitly before reassigning. Grouping never moves or deletes videos or changes the original account records. A former handle is an explicit user assertion about identity, not a claim about who currently controls that handle. Adding a profile does not crawl X or download posts.

Shared context loads when its section becomes visible. Up to 15 windows from the selected accounts and 15 related windows are shown. Related results must share at least two stored topics, display those topics, and are labeled inferred. Results can be searched and opened at their timestamp or loaded as a range in Cut.

Mobile defaults to the list view with a large details drawer. Page-scoped graph simulations and observers are cleaned up during navigation. Identity mutations refresh graph data and status through Datastar SSE.

## Validation

- Go tests cover X profile parsing and invalid URLs, plus vault-to-graph attachment (wiki-only creators, self-link skip, bundle neighbors).
- Node tests cover creator collapsing, evidence aggregation, former-name search, filtered neighborhoods, expansion, and wiki links joining a grouped creator.
- Browser checks cover grouping the two user-identified Brittany Venti accounts, SSE preservation of the current document, invalid-post rejection, evidence watch links, shared context lazy loading, and a 390px viewport.

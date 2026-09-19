# Library

The library is every video Rewind has archived, plus the people and channels those videos belong to.

## Videos

Open **Videos** for the grid. Search matches titles, uploaders, tags, comments, transcripts, and generated context windows.

Useful filters:

- **Uploader** — one channel
- **Tags** — comma-separated
- **Duration**, **has clips**, **has markers**
- Sort by archived date, published date, duration, or size

Click a card to open the watch page. From a channel or creator page, **Videos** can already be filtered to that uploader.

## Channels

**Channels** lists every uploader in the archive. Open a channel to see its videos, harvested outlinks and mentions, wiki pages, and follow controls.

Channel identity is the platform account (YouTube channel, X profile, and so on), not a person. One person with accounts on several sites is a **creator**.

To archive a whole channel or playlist, paste the URL on **Home**. Already-archived videos are skipped.

## Creators

**Creators** groups a person's channels across platforms. Confirm suggested alt/main links, or create a creator yourself and attach channels.

Use a creator when you want one search, one wiki home, and one follow set for someone who publishes in more than one place.

## Follows

**Follows** watches a channel URL on a schedule and queues new uploads. Enable, pause, or remove a follow without deleting videos you already have.

Follows are how Rewind does recurring archival. Worker counts and cookies still live in Admin / Settings; the follow itself is just the watch list.

## Network

**Network** is a live graph of outlinks, mentions, comments, and wiki links between archived channels. Same-creator channels cluster together.

Select a node to inspect evidence, open supporting videos, follow the account, or jump to attached wiki pages. Harvested edge weights are observation counts, not proof of a relationship.

## Visual search

**Visual search** finds scenes by a text description or a reference image after videos have been visually indexed. Lexical transcript search stays on **Videos**. Indexing is optional and runs in `rewind-ml`.

# Page navigation and live UI

The server still owns each page through the existing authenticated MPA routes.
Direct requests return a complete document. Internal links use DataStar GET with
`X-Rewind-Navigation: true`; the same route returns SSE that replaces only
`#page-content`. Navbar, appearance signals, assistant draft and assistant stream
remain in the document shell. Browser history, titles, focus and scroll position
are updated by `static/js/lib/navigation.js`.

Videos, Channels, Creators and Follows are direct navbar links. They stay visible
in a dedicated second row on narrow screens; secondary destinations remain in
the menu. Verified at 1280px and 390px without horizontal overflow, including
Channels navigation preserving the document and navbar while patching content.

## Coverage

Content navigation covers home, videos (including cut), channels, creators,
follows, visual search, people, network, compilations, stitch, show notes, jobs,
upload, settings, administration and producer pages. Ordinary links rendered by
the assistant use the same delegated navigation. Downloads, media, external
links, auth/session endpoints, modified clicks, new tabs and explicitly handled
actions keep their native behavior. Existing POST forms remain server actions;
the navigation middleware only handles GET page requests.

## Resource ownership

Page bundles use `lib/page-scope.js` for document/window listeners, timers,
animation frames, observers and cancellable fetches. Register editor/provider,
media and WebRTC teardown with `onPageCleanup`. Inline page scripts use
`RewindPage.ready`, `listen` and `eventSource`. Initialize on both direct loads
and inserted scripts; avoid document-global `const` declarations in inline page
scripts that run on repeat visits.

Cleanup happens only after the next page has rendered successfully. Failed page
requests keep the current page alive. DataStar owns its declarative subscriptions
and removes them with the page elements. Custom job-log and collaborative event
streams keep their existing event formats and explicitly close on page exit.

Persistence must finish before teardown. Editors register an asynchronous
`RewindNavigation.beforeLeave` guard and unregister it on cleanup. Navigation
temporarily makes page content inert, awaits these guards, and only then requests
the next page. A failed save keeps the editor and its resources available.
Stitch uses a serialized save queue so edits during an in-flight save are retained;
its guard flushes the debounce and waits for all pending writes. Native unload
warns while edits remain unsaved. Do not put a pending write behind a disposable
timer without a corresponding flush guard.

The assistant is outside the page lifecycle. Opening it loads the current
conversation once; closing it hides the panel without cancelling the run. Its
content has one scroll region, with history and conversation as alternate views.
The composer stays visible. Activity disclosures use a stable run-specific ID
and `data-preserve-attr="open"` so live patches respect the user's toggle.

## Deferred work

Video transcript, comments, context, clip export status and job history load
when their panes become visible. ML job summaries load on intersection. Preview
video media loads on hover. Editors retain their existing page-specific bundles;
they are not loaded by the common shell.

## Validation

`make test` covers direct versus fragment page rendering, shell exclusion, flushed
templ output, redirects and error handling. Run
`node --test static/js/lib/*.test.js` for SponsorBlock filtering and page lifecycle
regressions. Browser checks should include repeated library/video/jobs navigation,
an unsent assistant draft across page changes, Back/Forward, a mobile drawer and
menu, and opening activity while a run streams. Full WebRTC calls and rendered
video exports require their own end-to-end workflows beyond navigation checks.

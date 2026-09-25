import { listen as pageListen, pageFetch, onPageCleanup } from './lib/page-scope.js';
import * as Y from 'yjs';
import { WebsocketProvider } from 'y-websocket';
import { basicSetup } from 'codemirror';
import { EditorState, Prec, StateEffect, StateField } from '@codemirror/state';
import { EditorView, Decoration, ViewPlugin, WidgetType, keymap } from '@codemirror/view';
import { markdown } from '@codemirror/lang-markdown';
import { defaultKeymap, indentWithTab } from '@codemirror/commands';
import { yCollab, yUndoManagerKeymap } from 'y-codemirror.next';
import { parseShowNoteReference, parseTimestampCue, renderYouTubeCue, renderYouTubeEmbed } from './lib/shownote-embeds.js';

const pendingReferenceActions = new Set();

function referenceKey(reference) {
  return reference.key || reference.projection?.ID || reference.uri;
}

function refreshLivePreview() {
  window.__rewindShowDocument?.view.dispatch({ effects: refreshPreviewEffect.of(null) });
}

class MediaWidget extends WidgetType {
  constructor(reference, noteID) {
    super();
    this.reference = reference;
    this.noteID = noteID;
  }

  eq(other) {
    if (other.reference.kind === 'youtube' && this.reference.kind === 'youtube') {
      return other.reference.uri === this.reference.uri &&
        other.reference.time === this.reference.time &&
        other.reference.sourceKey === this.reference.sourceKey &&
        other.reference.start === this.reference.start &&
        other.reference.end === this.reference.end &&
        other.reference.label === this.reference.label &&
        other.reference.isCue === this.reference.isCue;
    }
    return other.reference.uri === this.reference.uri &&
      other.reference.time === this.reference.time &&
      other.reference.sourceKey === this.reference.sourceKey &&
      other.reference.start === this.reference.start &&
      other.reference.end === this.reference.end &&
      other.reference.label === this.reference.label &&
      other.reference.projection?.Status === this.reference.projection?.Status &&
      other.reference.projection?.Diagnostic === this.reference.projection?.Diagnostic;
  }

  toDOM() {
    const ref = this.reference;
    if (ref.kind === 'youtube') return ref.isCue ? renderYouTubeCue(ref.source, ref) : renderYouTubeEmbed(ref);
    const projection = ref.projection;
    const card = document.createElement('div');
    card.className = `sn-media-card${projection?.Diagnostic ? ' has-diagnostic' : ''}`;
    card.contentEditable = 'false';
    const icon = ref.kind === 'clip' ? 'fa-scissors' : ref.kind === 'marker' ? 'fa-bookmark' : 'fa-circle-play';
    const status = projection?.Diagnostic || projection?.Status || (ref.external ? 'Unresolved source' : 'Ready');
    card.innerHTML = `
      <div class="sn-media-thumb"><i class="fa-sharp fa-solid ${icon}"></i></div>
      <div class="sn-media-copy">
        <strong>${escapeHTML(ref.label || ref.uri)}</strong>
        <span>${escapeHTML(ref.time || 'Full video')} · ${status}</span>
      </div>
      <div class="sn-media-actions"></div>`;
    const actions = card.querySelector('.sn-media-actions');
    if (projection?.Diagnostic?.startsWith('displayed ') && projection.ID) {
      actions.appendChild(mediaActionButton('Refresh displayed time', ref, 'refresh_time'));
      actions.appendChild(mediaActionButton('Create new object', ref, projection.Kind === 'clip' ? 'create_clip' : 'create_marker'));
    } else if (ref.external && document.querySelector('[data-show-workspace]')?.dataset.live !== 'true' && (!projection || projection.Status === 'unresolved')) {
      const confirm = document.createElement('button');
      confirm.type = 'button';
      const pending = pendingReferenceActions.has(referenceKey(ref));
      confirm.disabled = pending;
      confirm.textContent = pending ? 'Resolving…' : 'Use match';
      confirm.addEventListener('click', (event) => {
        event.preventDefault();
        event.stopPropagation();
        pendingReferenceActions.add(referenceKey(ref));
        refreshLivePreview();
        window.dispatchEvent(new CustomEvent('rewind:resolve-reference', { detail: ref }));
      });
      actions.appendChild(confirm);
    } else if (!projection || projection.Status === 'ready') {
      const open = document.createElement('a');
      open.href = internalHref(ref);
      open.textContent = 'Open';
      actions.appendChild(open);
    }
    return card;
  }

  ignoreEvent() {
    return true;
  }

  destroy(dom) {
    dom?.__rewindDestroy?.();
  }
}

function mediaActionButton(label, reference, action) {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = label;
  button.addEventListener('click', () => {
    window.dispatchEvent(new CustomEvent('rewind:resolve-reference', { detail: { ...reference, action } }));
  });
  return button;
}

function escapeHTML(value) {
  return String(value).replace(/[&<>'"]/g, (ch) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' })[ch]);
}

function internalHref(ref) {
  if (ref.kind === 'video') return `/videos/${ref.id}`;
  if (ref.kind === 'clip') return `/videos/${ref.video || ''}/clips/${ref.id}`;
  return ref.uri;
}

const refreshPreviewEffect = StateEffect.define();

function buildLivePreview(state, noteID) {
  const ranges = [];
  const active = state.doc.lineAt(state.selection.main.head).number;
  let cueSource = null;
  for (let number = 1; number <= state.doc.lines; number += 1) {
    const line = state.doc.line(number);
    const heading = line.text.match(/^(#{1,6})\s+/);
    if (heading) cueSource = null;
    if (heading && line.number !== active) {
      ranges.push(Decoration.replace({}).range(line.from, line.from + heading[0].length));
      ranges.push(Decoration.line({ class: `sn-heading sn-heading-${heading[1].length}` }).range(line.from));
    }
    const parsedReference = parseShowNoteReference(line.text);
    let ref = parsedReference;
    if (!ref && cueSource) {
      const cue = parseTimestampCue(line.text);
      if (cue) ref = { ...cue, kind: 'youtube', isCue: true, source: cueSource, sourceKey: cueSource.sourceKey, uri: cueSource.uri, videoId: cueSource.videoId, providerURL: cueSource.providerURL, external: true, label: cue.label || cueSource.label };
      else cueSource = null;
    }
    if (parsedReference?.kind === 'youtube') cueSource = parsedReference;
    else if (parsedReference) cueSource = null;
    if (ref) {
      ref.projection = (window.__rewindReferences || []).find((candidate) => candidate.LineStart === line.number && candidate.SourceUri === ref.uri);
      // Projection IDs arrive asynchronously and can change during autosave.
      // Bind the player to the note occurrence so a projection refresh does
      // not destroy a playing iframe or lose its range timer.
      const occurrenceKey = `${line.number}:${ref.uri}`;
      ref.key = ref.projection?.ID || occurrenceKey;
      ref.sourceKey = `${noteID}:${occurrenceKey}`;
      const uriOffset = line.text.indexOf(ref.uri);
      if (uriOffset >= 0) {
        ranges.push(Decoration.mark({ class: 'sn-reference-link' }).range(line.from + uriOffset, line.from + uriOffset + ref.uri.length));
      }
    }
    if (ref) ranges.push(Decoration.widget({ widget: new MediaWidget(ref, noteID), block: true, side: 1 }).range(line.to));
  }
  return Decoration.set(ranges, true);
}

function livePreview(noteID) {
  const field = StateField.define({
    create(state) {
      return buildLivePreview(state, noteID);
    },
    update(decorations, transaction) {
      const selectionChanged = transaction.startState.selection.main.head !== transaction.state.selection.main.head;
      const refreshRequested = transaction.effects.some((effect) => effect.is(refreshPreviewEffect));
      return transaction.docChanged || selectionChanged || refreshRequested
        ? buildLivePreview(transaction.state, noteID)
        : decorations;
    },
    provide: (value) => EditorView.decorations.from(value),
  });
  const refreshListener = ViewPlugin.fromClass(class {
    constructor(view) {
      this.refresh = () => view.dispatch({ effects: refreshPreviewEffect.of(null) });
      pageListen(window, 'rewind:references-updated', this.refresh);
    }

    destroy() {
      window.removeEventListener('rewind:references-updated', this.refresh);
    }
  });
  return [field, refreshListener];
}

function decodeBytes(value) {
  if (!value || typeof value !== 'string') return null;
  try {
    return Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
  } catch (_) {
    return null;
  }
}

function reviewHighlights(ydoc) {
  return ViewPlugin.fromClass(class {
    constructor(view) {
      this.refresh = () => {
        this.decorations = this.build(view);
        view.dispatch({});
      };
      pageListen(window, 'rewind:reviews-updated', this.refresh);
      this.decorations = this.build(view);
    }

    update(update) {
      if (update.docChanged) this.decorations = this.build(update.view);
    }

    destroy() {
      window.removeEventListener('rewind:reviews-updated', this.refresh);
    }

    build(view) {
      const ranges = [];
      for (const review of (window.__rewindReviews || [])) {
        if (review.Status !== 'open' && review.Status !== 'stale') continue;
        const startBytes = decodeBytes(review.AnchorStart);
        const endBytes = decodeBytes(review.AnchorEnd);
        if (!startBytes || !endBytes) continue;
        try {
          const start = Y.createAbsolutePositionFromRelativePosition(Y.decodeRelativePosition(startBytes), ydoc);
          const end = Y.createAbsolutePositionFromRelativePosition(Y.decodeRelativePosition(endBytes), ydoc);
          if (start?.type === ydoc.getText('markdown') && end?.type === start.type && end.index > start.index && end.index <= view.state.doc.length) {
            ranges.push(Decoration.mark({ class: 'sn-comment-highlight', attributes: { 'data-review-id': review.ID } }).range(start.index, end.index));
          }
        } catch (_) {}
      }
      return Decoration.set(ranges, true);
    }
  }, { decorations: (value) => value.decorations });
}

function slashCommand(view) {
  const cursor = view.state.selection.main.head;
  const line = view.state.doc.lineAt(cursor);
  const command = line.text.trim().replace(/^(?:(?:\d+[.)]|[-+*])\s+)/, '');
  const replacements = { '/break': '> Break — ', '/heading': '# ' };
  let replacement = replacements[command];
  if (!replacement && ['/video', '/clip', '/marker'].includes(command)) {
    const kind = command.slice(1);
    const id = window.prompt(`${kind} ID or URL`);
    if (!id) return true;
    const label = window.prompt('Label', `Untitled ${kind}`) || `Untitled ${kind}`;
    const uri = /^https?:\/\//.test(id) ? id : `rewind://${kind}/${id}`;
    replacement = `[${label}](${uri}) @ 0:00`;
  }
  if (!replacement) return false;
  view.dispatch({ changes: { from: line.from, to: line.to, insert: replacement }, selection: { anchor: line.from + replacement.length } });
  return true;
}

function setupEditor(root) {
  const noteID = root.dataset.noteId;
  const doc = new Y.Doc();
  const ytext = doc.getText('markdown');
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const provider = new WebsocketProvider(`${proto}//${location.host}/api/show-notes/${noteID}/document`, noteID, doc, { connect: true });
  provider.awareness.setLocalStateField('user', {
    id: root.dataset.userId,
    name: root.dataset.username,
    color: root.dataset.color || '#60a5fa',
  });
  provider.awareness.setLocalStateField('panel', root.dataset.panel || 'notes');
  const undoManager = new Y.UndoManager(ytext);
  const state = EditorState.create({
    doc: '',
    extensions: [
      basicSetup,
      markdown(),
      EditorView.lineWrapping,
      EditorView.editable.of(root.dataset.role !== 'viewer'),
      Prec.highest(keymap.of([{ key: 'Tab', run: indentWithTab }, { key: 'Enter', run: slashCommand }, ...yUndoManagerKeymap, ...defaultKeymap])),
      yCollab(ytext, provider.awareness, { undoManager }),
      livePreview(noteID),
      reviewHighlights(doc),
      EditorView.updateListener.of((update) => {
        if (update.selectionSet) provider.awareness.setLocalStateField('panel', 'notes');
      }),
    ],
  });
  const view = new EditorView({ state, parent: root.querySelector('[data-editor]') });
  window.__rewindShowDocument = { doc, ytext, provider, view, undoManager };
  onPageCleanup(() => {
    roomEventSource?.close();
    view.destroy();
    provider.destroy();
    undoManager.destroy();
    doc.destroy();
    delete window.__rewindShowDocument;
  });
  provider.on('status', ({ status }) => setConnectionStatus(status));
  provider.on('sync', (synced) => setConnectionStatus(synced ? 'saved' : 'syncing'));
}

function setConnectionStatus(status) {
  document.querySelectorAll('[data-collab-status]').forEach((el) => {
    el.textContent = status === 'saved' || status === 'connected' ? 'Saved · live' : status;
    el.dataset.status = status;
  });
}

function applyLayout(layout) {
  const layouts = {
    planning: ['notes', 'room'],
    recording: ['notes', 'call', 'program'],
    directing: ['program', 'controls', 'rundown'],
  };
  const panels = ['notes', 'room', 'call', 'program', 'controls'];
  const visible = new Set(panels.includes(layout) ? [layout] : (layouts[layout] || layouts.planning));
  document.querySelectorAll('[data-workspace-panel]').forEach((panel) => {
    panel.hidden = !visible.has(panel.dataset.workspacePanel);
  });
  root.dataset.currentLayout = layout;
  document.querySelectorAll('[data-layout]').forEach((button) => button.classList.toggle('is-active', button.dataset.layout === layout));
  localStorage.setItem(`rewind:show-layout:${document.body.dataset.showNoteId || ''}`, layout);
}

let roomRefreshStarted = 0;
let roomRefreshApplied = 0;
let referencesFingerprint = '';

async function refreshRoom(noteID) {
  const list = document.querySelector('[data-room-messages]');
  if (!list) return;
  const requestNumber = ++roomRefreshStarted;
  try {
    const response = await pageFetch(`/api/show-notes/${noteID}/workspace`);
    if (!response.ok) return;
    const state = await response.json();
    if (requestNumber < roomRefreshApplied) return;
    roomRefreshApplied = requestNumber;
    const nextReferences = state.references || [];
    const nextFingerprint = JSON.stringify(nextReferences);
    if (nextFingerprint !== referencesFingerprint) {
      referencesFingerprint = nextFingerprint;
      window.__rewindReferences = nextReferences;
      window.dispatchEvent(new Event('rewind:references-updated'));
    }
    window.__rewindReviews = state.reviews || [];
    window.dispatchEvent(new Event('rewind:reviews-updated'));
    list.innerHTML = '';
    for (const message of (state.messages || []).reverse()) {
      const row = document.createElement('article');
      row.className = 'sn-room-message';
      row.innerHTML = `<header>${escapeHTML(message.ActorName || 'Participant')} <time>${new Date(message.CreatedAt).toLocaleTimeString()}</time></header><p>${escapeHTML(message.Body)}</p>`;
      list.appendChild(row);
    }
    const reviews = document.querySelector('[data-review-threads]');
    if (reviews) renderReviews(reviews, state.reviews || [], state.replies || {}, noteID, root.dataset.role !== 'viewer');
    return state;
  } catch (_) {}
}

let roomEventSource;
let pushedRefreshTimer;

function schedulePushedRefresh(noteID) {
  window.clearTimeout(pushedRefreshTimer);
  pushedRefreshTimer = window.setTimeout(() => refreshRoom(noteID), 75);
}

async function settleReferenceFromEvent(noteID, event) {
  if (document.querySelector('[data-show-workspace]')?.dataset.live === 'true') return;
  if (event.type !== 'reference_download_updated' || !event.payload?.video_id || !event.payload?.reference_id) return;
  await pageFetch(`/api/show-notes/${noteID}/references/${event.payload.reference_id}/materialize`, {
    method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ action: 'settle_archive' }),
  });
}

function connectRoomEvents(noteID, cursor) {
  if (roomEventSource) return;
  roomEventSource = new EventSource(`/api/show-notes/${noteID}/events?cursor=${encodeURIComponent(cursor)}`);
  roomEventSource.addEventListener('room', async (message) => {
    const event = JSON.parse(message.data);
    await settleReferenceFromEvent(noteID, event);
    schedulePushedRefresh(noteID);
  });
}

function renderReviews(container, reviews, replies, noteID, canReview) {
  container.innerHTML = '';
  for (const review of reviews) {
    const row = document.createElement('article');
    row.className = 'sn-review-thread';
    const title = review.Summary || (review.Kind === 'suggestion' ? 'Suggested edit' : 'Comment');
    row.innerHTML = `<header><strong>${escapeHTML(title)}</strong><span>${escapeHTML(review.Status)}</span></header>
      <p>${escapeHTML(review.Body || review.ExpectedText || '')}</p>
      ${review.Patch ? `<pre>${escapeHTML(review.Patch)}</pre>` : ''}<footer></footer>`;
    const footer = row.querySelector('footer');
    const replyList = document.createElement('div');
    replyList.className = 'sn-review-replies';
    for (const reply of (replies[review.ID] || [])) {
      const item = document.createElement('p');
      item.innerHTML = `<strong>${escapeHTML(reply.ActorName || 'Participant')}</strong> ${escapeHTML(reply.Body)}`;
      replyList.appendChild(item);
    }
    row.insertBefore(replyList, footer);
    if (canReview && review.Status === 'open') {
      if (review.Kind === 'suggestion') footer.appendChild(reviewActionButton(noteID, review.ID, 'accepted', 'Accept'));
      footer.appendChild(reviewActionButton(noteID, review.ID, review.Kind === 'suggestion' ? 'rejected' : 'resolved', review.Kind === 'suggestion' ? 'Reject' : 'Resolve'));
    } else if (canReview && (review.Status === 'resolved' || review.Status === 'rejected')) {
      footer.appendChild(reviewActionButton(noteID, review.ID, 'open', 'Reopen'));
    }
    if (canReview) footer.appendChild(reviewReplyButton(noteID, review.ID));
    container.appendChild(row);
  }
}

function reviewReplyButton(noteID, threadID) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'btn-ghost btn-sm';
  button.textContent = 'Reply';
  button.addEventListener('click', async () => {
    const body = window.prompt('Reply to this review');
    if (!body?.trim()) return;
    const response = await pageFetch(`/api/show-notes/${noteID}/reviews/${threadID}/replies`, {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ body }),
    });
    if (response.ok) await refreshRoom(noteID);
  });
  return button;
}

function reviewActionButton(noteID, threadID, status, label) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'btn-ghost btn-sm';
  button.textContent = label;
  button.addEventListener('click', async () => {
    await pageFetch(`/api/show-notes/${noteID}/reviews/${threadID}/status`, {
      method: 'PUT', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ status }),
    });
    await refreshRoom(noteID);
  });
  return button;
}

function selectRoomView(name) {
  document.querySelectorAll('[data-room-view]').forEach((view) => view.classList.toggle('hidden', view.dataset.roomView !== name));
  document.querySelectorAll('[data-room-tab]').forEach((tab) => {
    const active = tab.dataset.roomTab === name;
    tab.classList.toggle('text-white', active);
    tab.classList.toggle('border-b', active);
    tab.classList.toggle('border-white', active);
    tab.classList.toggle('text-white/35', !active);
  });
}

function editorRange(view) {
  const selection = view.state.selection.main;
  if (selection.empty) return null;
  const startLine = view.state.doc.lineAt(selection.from);
  const endLine = view.state.doc.lineAt(selection.to);
  return {
    expected_text: view.state.sliceDoc(selection.from, selection.to),
    start_line: startLine.number,
    start_column: Array.from(view.state.sliceDoc(startLine.from, selection.from)).length + 1,
    end_line: endLine.number,
    end_column: Array.from(view.state.sliceDoc(endLine.from, selection.to)).length + 1,
  };
}

async function handleSelectionAction(root, action) {
  const view = window.__rewindShowDocument?.view;
  const range = view && editorRange(view);
  if (!range) return;
  if (action === 'ask') {
    selectRoomView('room');
    const input = document.querySelector('[data-room-form] textarea');
    if (input) {
      input.value = `About “${range.expected_text}”: `;
      input.focus();
    }
    return;
  }
  const body = window.prompt('Comment on this selection');
  if (!body) return;
  const stateResponse = await pageFetch(`/api/show-notes/${root.dataset.noteId}/workspace`);
  if (!stateResponse.ok) return;
  const state = await stateResponse.json();
  const response = await pageFetch(`/api/show-notes/${root.dataset.noteId}/reviews`, {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ kind: 'comment', body, base_revision: state.document.Revision, ...range }),
  });
  if (response.ok) {
    selectRoomView('review');
    await refreshRoom(root.dataset.noteId);
  }
}

function setupRoom(root) {
  const noteID = root.dataset.noteId;
  const form = document.querySelector('[data-room-form]');
  if (form) form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const input = form.querySelector('textarea');
    const body = input.value.trim();
    if (!body) return;
    const response = await pageFetch(`/api/show-notes/${noteID}/messages`, {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ body }),
    });
    if (response.ok) input.value = '';
    await refreshRoom(noteID);
  });
  document.querySelectorAll('[data-room-tab]').forEach((tab) => tab.addEventListener('click', () => selectRoomView(tab.dataset.roomTab)));
  refreshRoom(noteID).then((state) => connectRoomEvents(noteID, state?.cursor || state?.Cursor || 0));
}

function setupWorkspace(root) {
  document.body.dataset.showNoteId = root.dataset.noteId;
  setupEditor(root);
  setupRoom(root);
  const requested = new URLSearchParams(location.search).get('layout');
  const saved = root.dataset.panel || requested || localStorage.getItem(`rewind:show-layout:${root.dataset.noteId}`) || 'planning';
  applyLayout(saved);
  document.querySelectorAll('[data-layout]').forEach((button) => button.addEventListener('click', () => applyLayout(button.dataset.layout)));
  document.querySelectorAll('[data-command]').forEach((button) => button.addEventListener('click', () => {
    const view = window.__rewindShowDocument && window.__rewindShowDocument.view;
    if (!view) return;
    view.dispatch({ changes: { from: view.state.selection.main.head, insert: button.dataset.command } });
    view.focus();
  }));
  document.querySelectorAll('[data-selection-action]').forEach((button) => button.addEventListener('click', () => handleSelectionAction(root, button.dataset.selectionAction)));
  pageListen(window, 'rewind:resolve-reference', async (event) => {
    if (root.dataset.live === 'true' && event.detail?.external) return;
    const pendingKey = referenceKey(event.detail);
    pendingReferenceActions.add(pendingKey);
    refreshLivePreview();
    try {
      const stateResponse = await pageFetch(`/api/show-notes/${root.dataset.noteId}/workspace`);
      if (!stateResponse.ok) return;
      const state = await stateResponse.json();
      const reference = event.detail.projection || (state.references || []).find((candidate) => candidate.SourceUri === event.detail.uri);
      if (!reference) return;
      const action = event.detail.action || 'use_match';
      let response = await pageFetch(`/api/show-notes/${root.dataset.noteId}/references/${reference.ID}/materialize`, {
        method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ action }),
      });
      if (root.dataset.live !== 'true' && action === 'use_match' && response.status === 409 && window.confirm('This source is not archived yet. Add it to the download queue?')) {
        response = await pageFetch(`/api/show-notes/${root.dataset.noteId}/references/${reference.ID}/materialize`, {
          method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ action: 'archive' }),
        });
      }
      if (!response.ok) {
        const problem = await response.json().catch(() => ({ message: 'Unable to resolve reference' }));
        window.alert(problem.message || problem.error || 'Unable to resolve reference');
        return;
      }
      await refreshRoom(root.dataset.noteId);
    } finally {
      pendingReferenceActions.delete(pendingKey);
      refreshLivePreview();
    }
  });
}

const root = document.querySelector('[data-show-workspace]');
if (root) setupWorkspace(root);

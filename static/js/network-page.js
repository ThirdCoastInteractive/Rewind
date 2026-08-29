import * as d3 from 'd3';

const KIND_COLOR = {
  outlink: '#ffffff',
  mention: '#d4d4d4',
  commented: '#a3a3a3',
};

const LABEL_CHAR_W = 6.1;
const LABEL_H = 12;

function kindColor(kind) {
  return KIND_COLOR[kind] || '#ffffff';
}

function nodeRadius(d) {
  return 3.5 + Math.min(8, Math.sqrt(d.degree || 1) * 1.4);
}

function isUrlOnly(d) {
  return !d.archived && String(d.id || '').startsWith('url:');
}

function displayLabel(d) {
  let s = d.label || '';
  if (s.startsWith('@')) return s.length > 28 ? s.slice(0, 26) + '…' : s;
  if (/^https?:\/\//i.test(s)) {
    try {
      const u = new URL(s);
      s = u.hostname.replace(/^www\./, '');
      if (u.pathname && u.pathname !== '/') s += u.pathname.slice(0, 18);
    } catch {
      /* keep raw */
    }
  }
  if (s.length > 28) s = s.slice(0, 26) + '…';
  return s;
}

function linkId(l, end) {
  const v = l[end];
  return typeof v === 'object' ? v.id : v;
}

function seedLayout(nodes, links, width, height) {
  const cx = width / 2;
  const cy = height / 2;
  const maxR = Math.min(width, height) * 0.42;
  for (const n of nodes) {
    n.x = undefined;
    n.y = undefined;
  }

  const groups = new Map();
  for (const n of nodes) {
    if (!n.creatorId) continue;
    if (!groups.has(n.creatorId)) groups.set(n.creatorId, []);
    groups.get(n.creatorId).push(n);
  }
  const ranked = [...groups.values()].sort((a, b) => b.length - a.length);
  const golden = Math.PI * (3 - Math.sqrt(5));
  const nSlots = Math.max(1, ranked.length);
  ranked.forEach((members, i) => {
    const r = maxR * Math.sqrt((i + 0.4) / (nSlots + 0.6)) * (0.82 + Math.random() * 0.32);
    const a = i * golden + (Math.random() - 0.5) * 0.9;
    const x = cx + Math.cos(a) * r;
    const y = cy + Math.sin(a) * r;
    const spread = 16 + Math.sqrt(members.length) * 12;
    for (const n of members) {
      n.x = x + (Math.random() - 0.5) * spread;
      n.y = y + (Math.random() - 0.5) * spread;
    }
  });

  const byId = new Map(nodes.map((n) => [n.id, n]));
  for (let pass = 0; pass < 8; pass++) {
    let grew = false;
    for (const l of links) {
      const s = byId.get(linkId(l, 'source'));
      const t = byId.get(linkId(l, 'target'));
      if (!s || !t) continue;
      if (Number.isFinite(s.x) && !Number.isFinite(t.x)) {
        t.x = s.x + (Math.random() - 0.5) * 56;
        t.y = s.y + (Math.random() - 0.5) * 56;
        grew = true;
      } else if (Number.isFinite(t.x) && !Number.isFinite(s.x)) {
        s.x = t.x + (Math.random() - 0.5) * 56;
        s.y = t.y + (Math.random() - 0.5) * 56;
        grew = true;
      }
    }
    if (!grew) break;
  }

  for (const n of nodes) {
    if (Number.isFinite(n.x) && Number.isFinite(n.y)) continue;
    const angle = Math.random() * Math.PI * 2;
    const r = Math.sqrt(Math.random()) * maxR;
    n.x = cx + Math.cos(angle) * r;
    n.y = cy + Math.sin(angle) * r;
  }
}

function forceCreator() {
  let nodes = [];
  function force(alpha) {
    const groups = d3.group(
      nodes.filter((n) => n.creatorId),
      (n) => n.creatorId
    );
    for (const members of groups.values()) {
      if (!members || members.length < 2) continue;
      let sx = 0;
      let sy = 0;
      for (const n of members) {
        sx += n.x;
        sy += n.y;
      }
      const mx = sx / members.length;
      const my = sy / members.length;
      const rest = 20 + Math.sqrt(members.length) * 14;
      const k = alpha * (0.4 + 0.5 / Math.sqrt(members.length));
      for (const n of members) {
        const dx = mx - n.x;
        const dy = my - n.y;
        const dist = Math.hypot(dx, dy) || 1;
        const pull = ((dist - rest) / dist) * k;
        n.vx += dx * pull;
        n.vy += dy * pull;
      }
    }
  }
  force.initialize = (n) => {
    nodes = n;
  };
  return force;
}

function rectsOverlap(a, b, pad) {
  return !(
    a.x + a.w + pad < b.x ||
    b.x + b.w + pad < a.x ||
    a.y + a.h + pad < b.y ||
    b.y + b.h + pad < a.y
  );
}

function init() {
  const root = document.getElementById('network-graph');
  const dataEl = document.getElementById('network-graph-data');
  if (!root || !dataEl) return;

  let graph;
  try {
    graph = JSON.parse(dataEl.getAttribute('data-graph') || '{}');
  } catch {
    return;
  }
  const nodes = Array.isArray(graph.nodes) ? graph.nodes : [];
  const links = Array.isArray(graph.links) ? graph.links : [];
  if (nodes.length === 0) return;

  const degree = new Map();
  for (const n of nodes) degree.set(n.id, 0);
  for (const l of links) {
    degree.set(l.source, (degree.get(l.source) || 0) + 1);
    degree.set(l.target, (degree.get(l.target) || 0) + 1);
  }
  for (const n of nodes) n.degree = degree.get(n.id) || 0;

  let selectedId = null;
  let ticks = 0;
  let fitted = false;
  let viewTouched = false;

  const inspector = document.getElementById('network-inspector');
  const inspectorPanel = document.getElementById('network-inspector-panel');
  const filterInput = document.getElementById('network-filter');
  const kindChecks = Array.from(document.querySelectorAll('[data-network-kind]'));

  const width = Math.max(root.clientWidth || 0, 800);
  const height = Math.max(root.clientHeight || 0, 560);
  seedLayout(nodes, links, width, height);

  const svg = d3
    .select(root)
    .append('svg')
    .attr('width', '100%')
    .attr('height', '100%')
    .attr('viewBox', [0, 0, width, height])
    .attr('preserveAspectRatio', 'xMidYMid meet')
    .attr('class', 'block w-full h-full');

  svg
    .append('defs')
    .selectAll('marker')
    .data(Object.keys(KIND_COLOR))
    .join('marker')
    .attr('id', (d) => 'arrow-' + d)
    .attr('viewBox', '0 -4 8 8')
    .attr('refX', 12)
    .attr('refY', 0)
    .attr('markerWidth', 5)
    .attr('markerHeight', 5)
    .attr('orient', 'auto')
    .append('path')
    .attr('d', 'M0,-4L8,0L0,4')
    .attr('fill', (d) => kindColor(d));

  const g = svg.append('g');
  const hull = g.append('g').attr('class', 'creator-hulls');

  const simulation = d3
    .forceSimulation(nodes)
    .force(
      'link',
      d3
        .forceLink(links)
        .id((d) => d.id)
        .distance((d) => 38 + Math.min(30, Math.sqrt(d.weight || 1) * 6))
        .strength(0.55)
        .iterations(1)
    )
    .force(
      'charge',
      d3
        .forceManyBody()
        .strength((d) => (d.archived ? -64 : -32) - Math.min(100, Math.sqrt(d.degree || 1) * 16))
        .distanceMax(420)
    )
    .force('center', d3.forceCenter(width / 2, height / 2).strength(0.07))
    .force(
      'collide',
      d3
        .forceCollide()
        .radius((d) => nodeRadius(d) + 8)
        .strength(0.85)
    )
    .force('creator', forceCreator())
    .alphaDecay(0.008)
    .velocityDecay(0.38)
    .alphaMin(0.001)
    .alphaTarget(0.028);

  const link = g
    .append('g')
    .attr('fill', 'none')
    .selectAll('line')
    .data(links)
    .join('line')
    .attr('stroke', (d) => kindColor(d.kind))
    .attr('stroke-opacity', (d) => (d.kind === 'outlink' ? 0.88 : 0.72))
    .attr('stroke-width', (d) => Math.max(1.4, Math.min(4.5, Math.sqrt(d.weight || 1) * 1.25)))
    .attr('stroke-linecap', 'round')
    .attr('marker-end', (d) => `url(#arrow-${d.kind in KIND_COLOR ? d.kind : 'outlink'})`);

  const node = g
    .append('g')
    .selectAll('circle')
    .data(nodes)
    .join('circle')
    .attr('r', (d) => nodeRadius(d))
    .attr('fill', (d) => (d.archived ? '#ffffff' : '#000000'))
    .attr('stroke', (d) => (d.archived ? '#ffffff' : '#a3a3a3'))
    .attr('stroke-width', (d) => (d.archived ? 1.5 : 1.2))
    .attr('cursor', 'pointer')
    .call(
      d3
        .drag()
        .clickDistance(5)
        .on('start', (event, d) => {
          if (!event.active) simulation.alphaTarget(0.18).restart();
          d.fx = d.x;
          d.fy = d.y;
          if (event.sourceEvent) event.sourceEvent.stopPropagation();
        })
        .on('drag', (event, d) => {
          viewTouched = true;
          d.fx = event.x;
          d.fy = event.y;
        })
        .on('end', (event, d) => {
          if (!event.active) simulation.alphaTarget(0.028);
          d.fx = null;
          d.fy = null;
        })
    );

  const label = g
    .append('g')
    .selectAll('text')
    .data(nodes)
    .join('text')
    .text((d) => displayLabel(d))
    .attr('font-family', 'Tomorrow, monospace')
    .attr('font-size', (d) => (d.archived && d.degree >= 3 ? 11 : 10))
    .attr('fill', (d) => (d.archived ? '#ffffff' : '#a3a3a3'))
    .attr('stroke', '#000000')
    .attr('stroke-width', 3)
    .attr('paint-order', 'stroke')
    .attr('pointer-events', 'none')
    .attr('dx', 7)
    .attr('dy', 3);

  node.append('title').text((d) => d.label + (d.platform ? ' · ' + d.platform : ''));

  const zoom = d3
    .zoom()
    .scaleExtent([0.15, 8])
    .filter((event) => {
      if (event.type === 'wheel') return true;
      const tag = event.target && event.target.tagName;
      if (tag === 'circle' || tag === 'text' || tag === 'line' || tag === 'path') return false;
      return (!event.ctrlKey || event.type === 'wheel') && !event.button;
    })
    .on('zoom', (event) => {
      g.attr('transform', event.transform);
      if (event.sourceEvent) viewTouched = true;
    });
  svg.call(zoom);
  svg.on('dblclick.zoom', null);

  svg.on('click', (event) => {
    if (event.target === svg.node()) selectNode(null);
  });

  node.on('click', (event, d) => {
    event.preventDefault();
    event.stopPropagation();
    viewTouched = true;
    selectNode(d);
  });

  function redraw() {
    link
      .attr('x1', (d) => d.source.x)
      .attr('y1', (d) => d.source.y)
      .attr('x2', (d) => d.target.x)
      .attr('y2', (d) => d.target.y);
    node.attr('cx', (d) => d.x).attr('cy', (d) => d.y);
    label.attr('x', (d) => d.x).attr('y', (d) => d.y);
    const hullData = [];
    const groups = d3.group(
      nodes.filter((n) => n.creatorId),
      (n) => n.creatorId
    );
    for (const [cid, members] of groups) {
      if (!members || members.length < 2) continue;
      const pts = members.map((n) => [n.x, n.y]).filter((p) => Number.isFinite(p[0]));
      if (pts.length < 2) continue;
      const xs = pts.map((p) => p[0]);
      const ys = pts.map((p) => p[1]);
      const span = Math.hypot(Math.max(...xs) - Math.min(...xs), Math.max(...ys) - Math.min(...ys));
      if (span > 340) continue;
      let poly = pts.length === 2 ? [pts[0], pts[1], [pts[0][0] + 8, pts[0][1] + 8]] : d3.polygonHull(pts);
      if (poly) hullData.push({ id: cid, path: poly });
    }
    hull
      .selectAll('path')
      .data(hullData, (d) => d.id)
      .join('path')
      .attr('fill', 'rgba(255,255,255,0.03)')
      .attr('stroke', '#ffffff')
      .attr('stroke-opacity', 0.35)
      .attr('stroke-dasharray', '6 4')
      .attr('stroke-width', 1.5)
      .attr('d', (d) => 'M' + d.path.map((p) => p.join(',')).join('L') + 'Z');
  }

  function fitGraph() {
    if (viewTouched || fitted) return;
    const xs = nodes.map((n) => n.x);
    const ys = nodes.map((n) => n.y);
    const minX = Math.min(...xs);
    const maxX = Math.max(...xs);
    const minY = Math.min(...ys);
    const maxY = Math.max(...ys);
    const bw = Math.max(40, maxX - minX + 120);
    const bh = Math.max(40, maxY - minY + 80);
    const scale = Math.min(width / bw, height / bh, 2.2) * 0.92;
    const tx = (width - scale * (minX + maxX)) / 2;
    const ty = (height - scale * (minY + maxY)) / 2;
    svg.transition().duration(0).call(zoom.transform, d3.zoomIdentity.translate(tx, ty).scale(scale));
    fitted = true;
  }

  simulation.on('tick', () => {
    ticks += 1;
    redraw();
    if (ticks % 12 === 0) resolveLabels();
    if (!fitted && !viewTouched && ticks === 80) fitGraph();
  });

  function visibleKinds() {
    const on = new Set();
    for (const el of kindChecks) {
      if (el.checked) on.add(el.getAttribute('data-network-kind'));
    }
    return on;
  }

  function neighborSet(id) {
    const s = new Set();
    if (!id) return s;
    for (const l of links) {
      const sid = linkId(l, 'source');
      const tid = linkId(l, 'target');
      if (sid === id) s.add(tid);
      if (tid === id) s.add(sid);
    }
    return s;
  }

  function labelPriority(d, matchNode, neighbors) {
    if (selectedId && d.id === selectedId) return 1000;
    if (selectedId && neighbors.has(d.id)) return 900;
    const q = (filterInput?.value || '').trim();
    if (q && matchNode.has(d.id)) return 800 + d.degree;
    if (d.archived) return 200 + d.degree;
    if (d.degree >= 2 && !isUrlOnly(d)) return 80 + d.degree;
    if (d.degree >= 2) return 40 + d.degree;
    return 0;
  }

  function resolveLabels() {
    const q = (filterInput?.value || '').trim().toLowerCase();
    const matchNode = new Set();
    for (const n of nodes) {
      if (!q || n.label.toLowerCase().includes(q) || (n.platform || '').toLowerCase().includes(q)) {
        matchNode.add(n.id);
      }
    }
    const neighbors = neighborSet(selectedId);
    const placed = [];
    const order = nodes
      .map((n) => ({ n, prio: labelPriority(n, matchNode, neighbors) }))
      .filter((x) => x.prio > 0)
      .sort((a, b) => b.prio - a.prio);

    const shown = new Set();
    for (const { n, prio } of order) {
      const force = prio >= 900;
      const box = {
        x: n.x + 7,
        y: n.y - LABEL_H / 2,
        w: Math.min(displayLabel(n).length * LABEL_CHAR_W, 170),
        h: LABEL_H,
      };
      const hits = placed.some((p) => rectsOverlap(box, p, 2));
      if (!force && hits) continue;
      shown.add(n.id);
      placed.push(box);
    }

    label.attr('opacity', (d) => {
      if (!shown.has(d.id)) return 0;
      if (selectedId && d.id !== selectedId && !neighbors.has(d.id)) {
        if (q && matchNode.has(d.id)) return 0.95;
        return d.archived ? 0.9 : 0.55;
      }
      return d.archived ? 1 : 0.75;
    });
  }

  function applyFilter() {
    const q = (filterInput?.value || '').trim().toLowerCase();
    const kinds = visibleKinds();
    const matchNode = new Set();
    for (const n of nodes) {
      if (!q || n.label.toLowerCase().includes(q) || (n.platform || '').toLowerCase().includes(q)) {
        matchNode.add(n.id);
      }
    }
    const neighbors = neighborSet(selectedId);

    link.attr('display', (d) => {
      const sid = linkId(d, 'source');
      const tid = linkId(d, 'target');
      const ok = kinds.has(d.kind) && matchNode.has(sid) && matchNode.has(tid);
      return ok ? null : 'none';
    });
    link.attr('stroke-opacity', (d) => {
      const sid = linkId(d, 'source');
      const tid = linkId(d, 'target');
      const base = d.kind === 'outlink' ? 0.88 : 0.72;
      if (!selectedId) return base;
      if (sid === selectedId || tid === selectedId) return 1;
      return 0.12;
    });

    node.attr('opacity', (d) => {
      const connected = links.some((l) => {
        const sid = linkId(l, 'source');
        const tid = linkId(l, 'target');
        return kinds.has(l.kind) && (sid === d.id || tid === d.id) && matchNode.has(sid) && matchNode.has(tid);
      });
      const inFilter = connected || matchNode.has(d.id);
      if (!inFilter) return 0.08;
      if (selectedId) {
        if (d.id === selectedId) return 1;
        if (neighbors.has(d.id)) return 1;
        return isUrlOnly(d) ? 0.12 : 0.18;
      }
      if (isUrlOnly(d)) return 0.38;
      return d.archived ? 1 : 0.55;
    });
    node.attr('stroke-width', (d) => (d.id === selectedId ? 3 : d.archived ? 1.5 : 1.2));
    resolveLabels();
  }

  function selectNode(d) {
    selectedId = d ? d.id : null;
    applyFilter();
    if (inspectorPanel) inspectorPanel.classList.toggle('hidden', !d);
    if (!inspector) return;
    if (!d) {
      inspector.innerHTML = '';
      return;
    }
    const incoming = [];
    const outgoing = [];
    for (const l of links) {
      const sid = linkId(l, 'source');
      const tid = linkId(l, 'target');
      const src = nodes.find((n) => n.id === sid);
      const tgt = nodes.find((n) => n.id === tid);
      if (tid === d.id) incoming.push({ l, peer: src });
      if (sid === d.id) outgoing.push({ l, peer: tgt });
    }
    const bits = [];
    bits.push(`<div class="font-mono text-sm text-white truncate">${esc(d.label)}</div>`);
    bits.push(
      `<div class="font-mono text-xs text-white/40 mt-0.5">${esc(d.platform || 'unknown')}${d.archived ? ' · archived' : ' · not archived'}${d.creatorName ? ' · creator ' + esc(d.creatorName) : ''}</div>`
    );
    if (d.creatorId) {
      const sibs = nodes.filter((n) => n.creatorId === d.creatorId && n.id !== d.id);
      if (sibs.length) {
        bits.push(`<div class="mt-2 font-mono text-xs text-white/40">Same creator</div>`);
        for (const s of sibs) {
          bits.push(
            `<div class="font-mono text-xs text-white truncate">${esc(s.label)} <span class="text-white/30">${esc(s.platform)}</span></div>`
          );
        }
      }
    }
    if (d.href) {
      bits.push(
        `<a href="${esc(d.href)}" class="inline-block mt-2 ghost-btn-sm text-white/60 border-white/40 hover:border-white hover:text-white">Open channel</a>`
      );
    }
    bits.push(renderEdgeGroup('Out', outgoing));
    bits.push(renderEdgeGroup('In', incoming));
    inspector.innerHTML = bits.join('');
  }

  function renderEdgeGroup(title, rows) {
    if (!rows.length) return '';
    const items = rows
      .sort((a, b) => (b.l.weight || 0) - (a.l.weight || 0))
      .slice(0, 24)
      .map((row) => {
        const name = row.peer ? row.peer.label : 'unknown';
        return `<div class="py-1.5 border-b border-white/10">
          <div class="flex items-center gap-2">
            <span class="badge">${esc(row.l.kind)}</span>
            <span class="font-mono text-xs text-white truncate">${esc(name)}</span>
            <span class="font-mono text-xs text-white/30 ml-auto">×${row.l.weight || 1}</span>
          </div>
          ${row.l.evidence ? `<div class="font-mono text-[10px] text-white/40 mt-0.5 truncate">${esc(row.l.evidence)}</div>` : ''}
        </div>`;
      })
      .join('');
    return `<div class="mt-3"><div class="font-mono text-xs uppercase tracking-wider text-white/40 mb-1">${title} (${rows.length})</div>${items}</div>`;
  }

  function esc(s) {
    return String(s || '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  filterInput?.addEventListener('input', applyFilter);
  for (const el of kindChecks) el.addEventListener('change', applyFilter);
  document.getElementById('network-inspector-close')?.addEventListener('click', (event) => {
    event.stopPropagation();
    selectNode(null);
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && selectedId) selectNode(null);
  });
  applyFilter();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}

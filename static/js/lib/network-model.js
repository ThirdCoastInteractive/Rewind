export function buildNetwork(raw, grouped = true, hanging = false) {
  const originals = (raw.nodes || []).filter(n => hanging || !n.id.startsWith('url:'));
  const mapping = new Map();
  const nodes = new Map();
  for (const source of originals) {
    const id = grouped && source.creatorId ? `creator:${source.creatorId}` : source.id;
    mapping.set(source.id, id);
    if (source.creatorId) mapping.set(`creator:${source.creatorId}`, id);
    if (!nodes.has(id)) nodes.set(id, { ...source, id, label: (grouped && source.creatorId ? source.creatorName : source.label) || source.id, members: [], degree: 0, evidenceCount: 0 });
    const node = nodes.get(id);
    node.members.push(source);
    node.archived ||= source.archived;
  }
  const edges = new Map();
  for (const evidence of raw.links || []) {
    const source = mapping.get(evidence.source), target = mapping.get(evidence.target);
    if (!source || !target || source === target) continue;
    const key = JSON.stringify([source, target, evidence.kind]);
    if (!edges.has(key)) edges.set(key, { source, target, kind: evidence.kind, weight: 0, evidence: [] });
    const edge = edges.get(key);
    edge.weight += evidence.weight || 1;
    edge.evidence.push(evidence);
  }
  for (const edge of edges.values()) {
    nodes.get(edge.source).degree++; nodes.get(edge.target).degree++;
    nodes.get(edge.source).evidenceCount += edge.weight;
    nodes.get(edge.target).evidenceCount += edge.weight;
  }
  return { nodes: [...nodes.values()], links: [...edges.values()] };
}

const COMMENTER_KINDS = new Set(['commented_by', 'campaign', 'style']);

export function visibleNetwork(graph, query, kinds, focus, hops = 1) {
  const endpoint = n => typeof n === 'object' ? n.id : n;
  const commentersOn = [...COMMENTER_KINDS].some(k => kinds.has(k));
  let links = graph.links.filter(l => kinds.has(l.kind));
  const q = query.trim().toLowerCase();
  let ids = new Set(graph.nodes.filter(n => {
    if (!commentersOn && (n.type === 'commenter' || String(n.id || '').startsWith('commenter:'))) return false;
    return !q || [n.label, ...n.members.map(m => `${m.label} ${m.platform} ${m.search || ""}`)].join(' ').toLowerCase().includes(q);
  }).map(n => n.id));
  const seeds = focus ? new Set([focus]) : new Set(ids);
  if (focus || q) {
    ids = seeds;
    for (let step = 0; step < (focus ? hops : 1); step++) {
      const next = new Set(ids);
      for (const l of links) if (ids.has(endpoint(l.source)) || ids.has(endpoint(l.target))) { next.add(endpoint(l.source)); next.add(endpoint(l.target)); }
      ids = next;
    }
  }
  links = links.filter(l => ids.has(endpoint(l.source)) && ids.has(endpoint(l.target)));
  return { nodes: graph.nodes.filter(n => ids.has(n.id)), links };
}

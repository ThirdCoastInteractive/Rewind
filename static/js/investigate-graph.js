(function () {
  const root = document.getElementById('investigate-graph');
  if (!root) return;

  let data;
  try {
    data = JSON.parse(root.getAttribute('data-investigate-graph') || '{}');
  } catch {
    return;
  }

  const nodes = (data.nodes || []).map((n) => ({ ...n, x: 0, y: 0, vx: 0, vy: 0 }));
  if (!nodes.length) return;

  const byId = Object.fromEntries(nodes.map((n) => [n.id, n]));
  const links = (data.links || [])
    .map((l) => ({
      source: byId[typeof l.source === 'string' ? l.source : l.source?.id],
      target: byId[typeof l.target === 'string' ? l.target : l.target?.id],
      kind: l.kind || '',
    }))
    .filter((l) => l.source && l.target);

  const width = Math.max(root.clientWidth, 320);
  const height = Math.max(root.clientHeight, 240);
  nodes.forEach((n, i) => {
    const a = (i / Math.max(nodes.length, 1)) * Math.PI * 2;
    n.x = width / 2 + Math.cos(a) * Math.min(width, height) * 0.22;
    n.y = height / 2 + Math.sin(a) * Math.min(width, height) * 0.22;
  });

  const css = getComputedStyle(root);
  const rgb = (name, fallback) => {
    const v = css.getPropertyValue(name).trim();
    return v ? `rgb(${v})` : fallback;
  };
  const ink = rgb('--rw-ink-rgb', '#fff');
  const muted = rgb('--rw-muted-rgb', '#aeaeae');
  const accent = rgb('--rw-accent-rgb', '#fff');
  const canvas = rgb('--rw-canvas-rgb', '#000');
  const line = rgb('--rw-line-rgb', '#484848');

  const svgNS = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(svgNS, 'svg');
  svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', 'Investigation graph of actors and campaigns');
  const layer = document.createElementNS(svgNS, 'g');
  svg.appendChild(layer);
  root.replaceChildren(svg);

  const lineEls = links.map((l) => {
    const el = document.createElementNS(svgNS, 'line');
    const user = l.kind === 'user';
    const style = l.kind === 'style';
    el.setAttribute('stroke', user ? accent : style ? muted : line);
    el.setAttribute('stroke-opacity', style ? '0.55' : '0.85');
    el.setAttribute('stroke-width', user ? '2.4' : '1.3');
    if (style) el.setAttribute('stroke-dasharray', '4 3');
    const title = document.createElementNS(svgNS, 'title');
    title.textContent = l.kind || 'edge';
    el.appendChild(title);
    layer.appendChild(el);
    return el;
  });

  const nodeEls = nodes.map((n) => {
    const g = document.createElementNS(svgNS, 'g');
    g.setAttribute('cursor', 'pointer');
    g.setAttribute('tabindex', '0');
    g.setAttribute('role', 'link');
    g.setAttribute('aria-label', `${n.kind || 'node'} ${n.label || n.id}`);
    const campaign = n.kind === 'campaign';
    const shape = document.createElementNS(svgNS, campaign ? 'rect' : 'circle');
    if (campaign) {
      shape.setAttribute('x', '-8');
      shape.setAttribute('y', '-8');
      shape.setAttribute('width', '16');
      shape.setAttribute('height', '16');
      shape.setAttribute('fill', accent);
    } else {
      shape.setAttribute('r', '7');
      shape.setAttribute('fill', ink);
    }
    shape.setAttribute('stroke', canvas);
    shape.setAttribute('stroke-width', '2');
    const text = document.createElementNS(svgNS, 'text');
    text.setAttribute('x', '12');
    text.setAttribute('y', '4');
    text.setAttribute('fill', ink);
    text.setAttribute('font-size', '11');
    text.textContent = n.label || n.id;
    const title = document.createElementNS(svgNS, 'title');
    title.textContent = [n.label, n.kind, n.meta].filter(Boolean).join(' · ');
    g.append(shape, text, title);
    const go = () => {
      if (n.href) window.location.href = n.href;
    };
    g.addEventListener('click', (e) => {
      if (g.dataset.dragged === '1') return;
      e.stopPropagation();
      go();
    });
    g.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        go();
      }
    });
    layer.appendChild(g);
    return g;
  });

  const tick = () => {
    for (const n of nodes) {
      if (n.fx == null) {
        n.vx *= 0.82;
        n.vy *= 0.82;
      }
    }
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i];
        const b = nodes[j];
        let dx = a.x - b.x;
        let dy = a.y - b.y;
        const d2 = dx * dx + dy * dy || 1;
        const f = 520 / d2;
        dx *= f;
        dy *= f;
        if (a.fx == null) {
          a.vx += dx;
          a.vy += dy;
        }
        if (b.fx == null) {
          b.vx -= dx;
          b.vy -= dy;
        }
      }
    }
    for (const l of links) {
      let dx = l.target.x - l.source.x;
      let dy = l.target.y - l.source.y;
      const dist = Math.hypot(dx, dy) || 1;
      const k = ((dist - 96) / dist) * 0.06;
      dx *= k;
      dy *= k;
      if (l.source.fx == null) {
        l.source.vx += dx;
        l.source.vy += dy;
      }
      if (l.target.fx == null) {
        l.target.vx -= dx;
        l.target.vy -= dy;
      }
    }
    for (const n of nodes) {
      if (n.fx == null) {
        n.vx += (width / 2 - n.x) * 0.012;
        n.vy += (height / 2 - n.y) * 0.012;
        n.x += n.vx;
        n.y += n.vy;
      } else {
        n.x = n.fx;
        n.y = n.fy;
        n.vx = 0;
        n.vy = 0;
      }
    }
    links.forEach((l, i) => {
      lineEls[i].setAttribute('x1', String(l.source.x));
      lineEls[i].setAttribute('y1', String(l.source.y));
      lineEls[i].setAttribute('x2', String(l.target.x));
      lineEls[i].setAttribute('y2', String(l.target.y));
    });
    nodes.forEach((n, i) => {
      nodeEls[i].setAttribute('transform', `translate(${n.x},${n.y})`);
    });
  };

  for (let i = 0; i < 90; i++) tick();

  let frames = 0;
  const loop = () => {
    if (frames++ > 200) return;
    tick();
    requestAnimationFrame(loop);
  };
  requestAnimationFrame(loop);

  let scale = 1;
  let tx = 0;
  let ty = 0;
  const applyView = () => {
    layer.setAttribute('transform', `translate(${tx} ${ty}) scale(${scale})`);
  };

  svg.addEventListener(
    'wheel',
    (e) => {
      e.preventDefault();
      const next = Math.min(4, Math.max(0.35, scale * (e.deltaY > 0 ? 0.92 : 1.08)));
      const rect = svg.getBoundingClientRect();
      const mx = ((e.clientX - rect.left) / rect.width) * width;
      const my = ((e.clientY - rect.top) / rect.height) * height;
      tx = mx - ((mx - tx) * next) / scale;
      ty = my - ((my - ty) * next) / scale;
      scale = next;
      applyView();
    },
    { passive: false }
  );

  let pan = null;
  svg.addEventListener('pointerdown', (e) => {
    if (e.target.closest('g[role="link"]')) return;
    pan = { x: e.clientX, y: e.clientY, tx, ty };
    svg.setPointerCapture(e.pointerId);
  });
  svg.addEventListener('pointermove', (e) => {
    if (!pan) return;
    const rect = svg.getBoundingClientRect();
    tx = pan.tx + ((e.clientX - pan.x) / rect.width) * width;
    ty = pan.ty + ((e.clientY - pan.y) / rect.height) * height;
    applyView();
  });
  const endPan = () => {
    pan = null;
  };
  svg.addEventListener('pointerup', endPan);
  svg.addEventListener('pointercancel', endPan);

  nodes.forEach((n, i) => {
    const g = nodeEls[i];
    let drag = null;
    g.addEventListener('pointerdown', (e) => {
      e.stopPropagation();
      g.dataset.dragged = '0';
      const rect = svg.getBoundingClientRect();
      drag = {
        x: e.clientX,
        y: e.clientY,
        px: n.x,
        py: n.y,
      };
      n.fx = n.x;
      n.fy = n.y;
      g.setPointerCapture(e.pointerId);
      const onMove = (ev) => {
        if (!drag) return;
        const dx = ((ev.clientX - drag.x) / rect.width) * width / scale;
        const dy = ((ev.clientY - drag.y) / rect.height) * height / scale;
        if (Math.hypot(ev.clientX - drag.x, ev.clientY - drag.y) > 4) g.dataset.dragged = '1';
        n.fx = drag.px + dx;
        n.fy = drag.py + dy;
        frames = 0;
        requestAnimationFrame(loop);
      };
      const onUp = () => {
        drag = null;
        n.fx = null;
        n.fy = null;
        g.removeEventListener('pointermove', onMove);
        g.removeEventListener('pointerup', onUp);
        frames = 0;
        requestAnimationFrame(loop);
      };
      g.addEventListener('pointermove', onMove);
      g.addEventListener('pointerup', onUp);
    });
  });
})();

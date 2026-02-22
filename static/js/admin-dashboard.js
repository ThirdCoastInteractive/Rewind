import * as d3 from 'd3';

// ============================================================================
// Admin Dashboard Charts (D3)
// CRT amber / cassette-futurism palette
// ============================================================================

const C = {
  // Primary CRT amber tones
  amber:     'oklch(.72 .20 75)',
  amberDim:  'oklch(.65 .18 75)',
  amberGlow: 'oklch(.78 .22 75)',
  // Terminal / UI chrome
  text:      'oklch(.75 .04 75)',
  textDim:   'oklch(.55 .03 75)',
  grid:      'oklch(.28 .02 75)',
  axis:      'oklch(.40 .03 75)',
  // Bar chart fill
  bar:       'oklch(.68 .19 75)',
  barHover:  'oklch(.78 .22 75)',
  // Palette for multi-series (boosted chroma + lightness)
  palette: [
    'oklch(.70 .22 75)',   // bright amber
    'oklch(.55 .28 149)',  // vivid green
    'oklch(.65 .22 210)',  // electric teal
    'oklch(.72 .20 65)',   // warm gold
    'oklch(.60 .26 135)',  // lime green
    'oklch(.65 .24 235)',  // bright blue
    'oklch(.68 .18 55)',   // taupe gold
    'oklch(.55 .22 165)',  // deep teal
    'oklch(.62 .20 164)',  // sea green
    'oklch(.65 .18 305)',  // mauve accent
  ],
};

const FONT = '"Tomorrow", monospace';

// ============================================================================
// Archives per day (vertical bar chart)
// ============================================================================

export function renderVideosPerDayChart(selector, data) {
  const container = document.querySelector(selector);
  if (!container || !data || data.length === 0) return;

  container.innerHTML = '';

  const rect = container.getBoundingClientRect();
  const margin = { top: 16, right: 12, bottom: 28, left: 32 };
  const width = rect.width - margin.left - margin.right;
  const height = rect.height - margin.top - margin.bottom;
  if (width <= 0 || height <= 0) return;

  const svg = d3.select(selector)
    .append('svg')
    .attr('width', rect.width)
    .attr('height', rect.height)
    .append('g')
    .attr('transform', `translate(${margin.left},${margin.top})`);

  const x = d3.scaleBand()
    .domain(data.map(d => d.day))
    .range([0, width])
    .padding(0.25);

  const maxCount = d3.max(data, d => d.count) || 1;
  const y = d3.scaleLinear()
    .domain([0, maxCount])
    .nice()
    .range([height, 0]);

  // Grid
  svg.append('g')
    .call(d3.axisLeft(y).ticks(Math.min(maxCount, 5)).tickSize(-width).tickFormat(''))
    .call(g => g.select('.domain').remove())
    .call(g => g.selectAll('.tick line')
      .attr('stroke', C.grid)
      .attr('stroke-dasharray', '2,3'));

  // X axis
  const dayFmt = d3.timeFormat('%-m/%d');
  svg.append('g')
    .attr('transform', `translate(0,${height})`)
    .call(d3.axisBottom(x)
      .tickFormat(d => dayFmt(new Date(d + 'T00:00:00')))
      .tickValues(x.domain().filter((_, i) => i % Math.max(1, Math.ceil(data.length / 7)) === 0))
    )
    .call(g => g.selectAll('text').attr('fill', C.textDim).attr('font-family', FONT).attr('font-size', '9px'))
    .call(g => g.select('.domain').attr('stroke', C.axis))
    .call(g => g.selectAll('.tick line').attr('stroke', C.axis));

  // Y axis
  svg.append('g')
    .call(d3.axisLeft(y).ticks(Math.min(maxCount, 5)).tickFormat(d3.format('d')))
    .call(g => g.selectAll('text').attr('fill', C.textDim).attr('font-family', FONT).attr('font-size', '9px'))
    .call(g => g.select('.domain').attr('stroke', C.axis))
    .call(g => g.selectAll('.tick line').attr('stroke', C.axis));

  // Bars
  svg.selectAll('.bar')
    .data(data)
    .enter()
    .append('rect')
    .attr('x', d => x(d.day))
    .attr('y', d => y(d.count))
    .attr('width', x.bandwidth())
    .attr('height', d => height - y(d.count))
    .attr('fill', C.bar)
    .attr('rx', 1)
    .on('mouseenter', function () { d3.select(this).attr('fill', C.barHover); })
    .on('mouseleave', function () { d3.select(this).attr('fill', C.bar); });

  // Count labels
  svg.selectAll('.label')
    .data(data.filter(d => d.count > 0))
    .enter()
    .append('text')
    .attr('x', d => x(d.day) + x.bandwidth() / 2)
    .attr('y', d => y(d.count) - 4)
    .attr('text-anchor', 'middle')
    .attr('fill', C.text)
    .attr('font-family', FONT)
    .attr('font-size', '8px')
    .text(d => d.count);
}

// ============================================================================
// Sources breakdown (horizontal bar chart)
// ============================================================================

export function renderSourcesChart(selector, data) {
  const container = document.querySelector(selector);
  if (!container || !data || data.length === 0) return;

  container.innerHTML = '';

  const rect = container.getBoundingClientRect();
  const margin = { top: 4, right: 40, bottom: 4, left: 100 };
  const width = rect.width - margin.left - margin.right;
  const height = rect.height - margin.top - margin.bottom;
  if (width <= 0 || height <= 0) return;

  const color = d3.scaleOrdinal().domain(data.map(d => d.source)).range(C.palette);

  const svg = d3.select(selector)
    .append('svg')
    .attr('width', rect.width)
    .attr('height', rect.height)
    .append('g')
    .attr('transform', `translate(${margin.left},${margin.top})`);

  const y = d3.scaleBand()
    .domain(data.map(d => d.source))
    .range([0, height])
    .padding(0.3);

  const maxCount = d3.max(data, d => d.count) || 1;
  const x = d3.scaleLinear()
    .domain([0, maxCount])
    .range([0, width]);

  // Labels
  svg.selectAll('.src-label')
    .data(data)
    .enter()
    .append('text')
    .attr('x', -8)
    .attr('y', d => y(d.source) + y.bandwidth() / 2)
    .attr('text-anchor', 'end')
    .attr('dominant-baseline', 'central')
    .attr('fill', C.text)
    .attr('font-family', FONT)
    .attr('font-size', '10px')
    .text(d => d.source);

  // Bars
  svg.selectAll('.bar')
    .data(data)
    .enter()
    .append('rect')
    .attr('x', 0)
    .attr('y', d => y(d.source))
    .attr('width', d => x(d.count))
    .attr('height', y.bandwidth())
    .attr('fill', d => color(d.source))
    .attr('rx', 1)
    .on('mouseenter', function () { d3.select(this).attr('opacity', 0.8); })
    .on('mouseleave', function () { d3.select(this).attr('opacity', 1); });

  // Count labels
  svg.selectAll('.count-label')
    .data(data)
    .enter()
    .append('text')
    .attr('x', d => x(d.count) + 6)
    .attr('y', d => y(d.source) + y.bandwidth() / 2)
    .attr('dominant-baseline', 'central')
    .attr('fill', C.textDim)
    .attr('font-family', FONT)
    .attr('font-size', '10px')
    .text(d => d.count);
}

// ============================================================================
// Storage by uploader (horizontal bar chart)
// ============================================================================

export function renderStorageChart(selector, data) {
  const container = document.querySelector(selector);
  if (!container || !data || data.length === 0) return;

  container.innerHTML = '';

  const rect = container.getBoundingClientRect();
  const margin = { top: 4, right: 56, bottom: 4, left: 120 };
  const width = rect.width - margin.left - margin.right;
  const height = rect.height - margin.top - margin.bottom;
  if (width <= 0 || height <= 0) return;

  const svg = d3.select(selector)
    .append('svg')
    .attr('width', rect.width)
    .attr('height', rect.height)
    .append('g')
    .attr('transform', `translate(${margin.left},${margin.top})`);

  const y = d3.scaleBand()
    .domain(data.map(d => d.uploader))
    .range([0, height])
    .padding(0.3);

  const maxBytes = d3.max(data, d => d.totalBytes) || 1;
  const x = d3.scaleLinear()
    .domain([0, maxBytes])
    .range([0, width]);

  // Uploader labels (truncate long names)
  svg.selectAll('.upl-label')
    .data(data)
    .enter()
    .append('text')
    .attr('x', -8)
    .attr('y', d => y(d.uploader) + y.bandwidth() / 2)
    .attr('text-anchor', 'end')
    .attr('dominant-baseline', 'central')
    .attr('fill', C.text)
    .attr('font-family', FONT)
    .attr('font-size', '10px')
    .text(d => d.uploader.length > 18 ? d.uploader.slice(0, 17) + '…' : d.uploader);

  // Bars
  svg.selectAll('.bar')
    .data(data)
    .enter()
    .append('rect')
    .attr('x', 0)
    .attr('y', d => y(d.uploader))
    .attr('width', d => x(d.totalBytes))
    .attr('height', y.bandwidth())
    .attr('fill', C.amberDim)
    .attr('rx', 1)
    .on('mouseenter', function () { d3.select(this).attr('fill', C.amberGlow); })
    .on('mouseleave', function () { d3.select(this).attr('fill', C.amberDim); });

  // Size labels
  svg.selectAll('.size-label')
    .data(data)
    .enter()
    .append('text')
    .attr('x', d => x(d.totalBytes) + 6)
    .attr('y', d => y(d.uploader) + y.bandwidth() / 2)
    .attr('dominant-baseline', 'central')
    .attr('fill', C.textDim)
    .attr('font-family', FONT)
    .attr('font-size', '10px')
    .text(d => formatBytes(d.totalBytes));
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

// ============================================================================
// Auto-initialize
// ============================================================================

function init() {
  const el = document.getElementById('admin-dashboard-data');
  if (!el) return;

  let data;
  try {
    data = JSON.parse(el.dataset.chart);
  } catch {
    return;
  }

  if (data.videosPerDay) {
    renderVideosPerDayChart('#chart-videos-per-day', data.videosPerDay);
  }
  if (data.topSources) {
    renderSourcesChart('#chart-sources', data.topSources);
  }
  if (data.storageByUploader) {
    renderStorageChart('#chart-storage', data.storageByUploader);
  }
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}

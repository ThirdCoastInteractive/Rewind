import * as d3 from 'd3';
import { listen, onPageCleanup, PageResizeObserver, PageMutationObserver } from './lib/page-scope.js';
import { buildNetwork, visibleNetwork } from './lib/network-model.js';
const root=document.getElementById('network-graph');
if(root) init();
function init(){
 const $=id=>document.getElementById(id), panel=$('network-inspector-panel'), inspector=$('network-inspector');
 const endpoint=n=>typeof n==='object'?n.id:n;
 const esc=v=>String(v||'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 let raw,graph,simulation,selected=null,hops=1,expanded=new Set();
 if(matchMedia('(max-width:767px)').matches)$('network-mode').value='list';
 function read(){raw=JSON.parse($('network-graph-data').dataset.graph);rebuild();}
 function rebuild(){const input={...raw,nodes:raw.nodes.map(n=>expanded.has(n.creatorId)?{...n,creatorId:'',creatorName:''}:n)};graph=buildNetwork(input,$('network-group').checked,$('network-hanging').checked);if(!graph.nodes.some(n=>n.id===selected))selected=null;draw();}
 const commenterKinds=['commented_by','campaign','style'];
 function activeKinds(){
  const kinds=new Set([...document.querySelectorAll('[data-network-kind]:checked')].map(n=>n.dataset.networkKind));
  const box=$('network-commenters');
  if(box?.checked) commenterKinds.forEach(k=>kinds.add(k));
  else commenterKinds.forEach(k=>kinds.delete(k));
  return kinds;
 }
 function syncCommenterFilter(){
  const box=$('network-commenters');
  if(!box||box.dataset.userTouched==='1'||!graph) return;
  const on=!!selected&&graph.links.some(l=>commenterKinds.includes(l.kind)&&(endpoint(l.source)===selected||endpoint(l.target)===selected));
  box.checked=on;
 }
 function view(){syncCommenterFilter();return visibleNetwork(graph,$('network-filter').value,activeKinds(),selected,hops);}
 function draw(){
  simulation?.stop();root.replaceChildren();const data=view();
  if($('network-mode').value==='graph'&&!selected&&!$('network-filter').value.trim())data.nodes=data.nodes.filter(n=>n.degree>0||n.members.length>1);
  $('network-count').textContent=`${data.nodes.length} creators / channels - ${data.links.length} connections`;
  renderList(data);const listMode=$('network-mode').value==='list';$('network-list').classList.toggle('hidden',!listMode);root.classList.toggle('hidden',listMode);if(listMode)return;
  const width=Math.max(root.clientWidth,320),height=Math.max(root.clientHeight,320);
  const svg=d3.select(root).append('svg').attr('width','100%').attr('height','100%').attr('role','img').attr('aria-label','Creator connections graph'),layer=svg.append('g');
  const zoom=d3.zoom().scaleExtent([.15,5]).on('zoom',e=>layer.attr('transform',e.transform));svg.call(zoom).on('dblclick.zoom',null);
  const nodes=data.nodes.map(n=>({...n})),links=data.links.map(l=>({...l,source:endpoint(l.source),target:endpoint(l.target)}));
  const stroke={outlink:'#e6c06b',mention:'#78bde8',commented:'#ba9ce8',wiki:'#7dcea0',commented_by:'#f0a0c0',campaign:'#f5a623',style:'#c792ea'};
  const line=layer.append('g').selectAll('line').data(links).join('line').attr('stroke',d=>stroke[d.kind]||'#fff').attr('stroke-width',d=>Math.min(7,2+Math.sqrt(d.weight))).attr('stroke-opacity',.7).attr('cursor','pointer').on('click',(e,d)=>{e.stopPropagation();showEdge(d);});
  line.append('title').text(d=>d.kind==='wiki'?`${d.kind}: vault wikilink. Click for the encyclopedia page.`:`${d.kind}: ${d.weight} observations. Click for evidence.`);
  const groups=layer.append('g').selectAll('g').data(nodes).join('g').attr('cursor','pointer').attr('tabindex',0).attr('role','button').attr('aria-label',d=>`Explore ${d.label}`).on('click',(e,d)=>{e.stopPropagation();focus(d);}).on('keydown',(e,d)=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();focus(d);}});
  groups.append('circle').attr('r',d=>d.type==='commenter'?7:d.members.length>1?12:8).attr('fill',d=>d.type==='commenter'?'#f0a0c0':d.members.length>1?'#e6c06b':d.archived?'#fff':'#171717').attr('stroke','#fff');
  groups.append('text').attr('x',16).attr('y',4).attr('fill','#fff').attr('font-size',13).attr('stroke','#111').attr('stroke-width',3).attr('paint-order','stroke').text(d=>d.label+(d.members.length>1?` (${d.members.length})`:''));
  simulation=d3.forceSimulation(nodes).force('link',d3.forceLink(links).id(d=>d.id).distance(180)).force('charge',d3.forceManyBody().strength(-550)).force('collision',d3.forceCollide(65)).force('center',d3.forceCenter(width/2,height/2));
  groups.call(d3.drag().on('start',(e,d)=>{simulation.alphaTarget(.1).restart();d.fx=d.x;d.fy=d.y;}).on('drag',(e,d)=>{d.fx=e.x;d.fy=e.y;}).on('end',(e,d)=>{simulation.alphaTarget(0);d.fx=null;d.fy=null;}));
  const tick=()=>{line.attr('x1',d=>d.source.x).attr('y1',d=>d.source.y).attr('x2',d=>d.target.x).attr('y2',d=>d.target.y);groups.attr('transform',d=>`translate(${d.x},${d.y})`);};
  simulation.stop();for(let i=0;i<100;i++)simulation.tick();tick();
  if(nodes.length){const xs=nodes.map(n=>n.x),ys=nodes.map(n=>n.y),minX=Math.min(...xs)-30,maxX=Math.max(...xs)+210,minY=Math.min(...ys)-30,maxY=Math.max(...ys)+30;const k=Math.min(1.2,width/(maxX-minX),height/(maxY-minY))*.92;svg.call(zoom.transform,d3.zoomIdentity.translate((width-k*(minX+maxX))/2,(height-k*(minY+maxY))/2).scale(k));}
  simulation.on('tick',tick);
 }
 function focus(n){
  selected=n.id;hops=1;draw();panel.classList.remove('hidden');inspector.innerHTML=`<p role="status">Loading ${n.type==='commenter'?'commenter':'creator'} details...</p>`;
  if(n.id.startsWith('url:')){inspector.replaceChildren();const title=document.createElement('h3');title.textContent=n.label;inspector.append(title);try{const u=new URL(n.id.slice(4));if(['https:','http:'].includes(u.protocol)){const a=document.createElement('a');a.href=u.href;a.target='_blank';a.rel='noopener';a.textContent='Open source profile';a.className='underline';inspector.append(a);}}catch{}return;}
  window.__dsAPI?.mergePatch({networkNode:n.id,networkContextQuery:''});document.querySelector('[data-network-inspect]')?.click();
 }
 function showEdge(edge){
  panel.classList.remove('hidden');const name=id=>graph.nodes.find(n=>n.id===endpoint(id))?.label||endpoint(id);
  const wiki=edge.kind==='wiki';
  const commenter=commenterKinds.includes(edge.kind);
  inspector.innerHTML=`<h3 class="font-bold">${esc(name(edge.source))} to ${esc(name(edge.target))}</h3><p class="text-xs my-2">${esc(edge.kind)} - ${wiki?'encyclopedia wikilink. Vault cross-references are memory, not harvested observations.':`${edge.weight} observations. ${commenter?'Commenter edges are observation counts, not proof of a relationship.':'Archived links and mentions do not establish a personal relationship.'}`}</p>`+edge.evidence.map(e=>`<article class="border-t border-white/20 py-3"><p class="text-sm whitespace-pre-wrap break-words">${esc(e.evidence||'No text excerpt was stored for this connection.')}</p>${e.href?`<a class="underline text-xs" href="${esc(e.href)}">Open vault page</a>`:e.videoId?`<a class="underline text-xs" href="/videos/${encodeURIComponent(e.videoId)}">Watch supporting video</a>`:'<p class="text-xs text-white/50">No supporting video was recorded.</p>'}</article>`).join('');
  const ask=document.createElement('button');ask.className='ghost-btn-sm';ask.textContent='Ask assistant about this connection';ask.onclick=()=>window.__dsAPI?.mergePatch({agentOpen:true,agentPrompt:`Explain the archived connection between ${name(edge.source)} and ${name(edge.target)}. Distinguish explicit evidence from inference. Supporting video IDs: ${edge.evidence.map(e=>e.videoId).filter(Boolean).join(', ')}.`});inspector.append(ask);
 }
 function renderList(data){
  const list=$('network-list');list.replaceChildren();const rows=[...data.nodes].sort((a,b)=>$('network-sort').value==='name'?a.label.localeCompare(b.label):b.evidenceCount-a.evidenceCount||a.label.localeCompare(b.label));
  if(!rows.length){list.textContent='No matching creators or connections. Try another search or show all.';return;}
  for(const n of rows){const article=document.createElement('article');article.className='border-b border-white/20 py-3';const b=document.createElement('button');b.className='font-bold text-left';b.textContent=n.label+(n.members.length>1?` - ${n.members.length} channels`:'');b.onclick=()=>focus(n);article.append(b);for(const edge of data.links.filter(l=>endpoint(l.source)===n.id)){const peer=graph.nodes.find(p=>p.id===endpoint(edge.target));const e=document.createElement('button');e.className='block text-left text-sm py-2 underline';e.textContent=`${edge.kind} to ${peer?.label} - ${edge.weight} observations`;e.onclick=()=>showEdge(edge);article.append(e);}list.append(article);}
 }
 listen($('network-bundle'),'change',()=>window.RewindNavigation.navigate('/network?bundle='+encodeURIComponent($('network-bundle').value)));
 listen($('network-filter'),'input',()=>{selected=null;draw();});for(const id of ['network-mode','network-sort'])listen($(id),'change',draw);for(const id of ['network-group','network-hanging'])listen($(id),'change',rebuild);for(const el of document.querySelectorAll('[data-network-kind]'))listen(el,'change',draw);
 const commentersBox=$('network-commenters');if(commentersBox)listen(commentersBox,'change',()=>{commentersBox.dataset.userTouched='1';draw();});
 listen($('network-reset'),'click',()=>{selected=null;hops=1;$('network-filter').value='';if(commentersBox){commentersBox.dataset.userTouched='';commentersBox.checked=false;}draw();});listen($('network-expand'),'click',()=>{if(selected){hops=Math.min(5,hops+1);draw();}});listen($('network-inspector-close'),'click',()=>panel.classList.add('hidden'));
 listen(inspector,'click',event=>{const expand=event.target.closest('[data-network-expand-creator]');if(expand){expanded.add(expand.dataset.networkExpandCreator);rebuild();}const button=event.target.closest('[data-network-focus]');if(button){const n=graph.nodes.find(n=>n.id===button.dataset.networkFocus||n.members.some(m=>m.id===button.dataset.networkFocus));if(n)focus(n);}});
 new PageMutationObserver(()=>read()).observe($('network-graph-data').parentElement,{childList:true});new PageResizeObserver(()=>draw()).observe(root);onPageCleanup(()=>simulation?.stop());read();
}

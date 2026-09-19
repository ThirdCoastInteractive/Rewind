import test from 'node:test';
import assert from 'node:assert/strict';
import { buildNetwork, visibleNetwork } from './network-model.js';

const raw = { nodes: [
  { id:'a', label:'Old handle', creatorId:'c', creatorName:'Creator' },
  { id:'b', label:'New handle', creatorId:'c', creatorName:'Creator' },
  { id:'d', label:'Neighbor' }, { id:'e', label:'Distant' },
], links:[
  {source:'a', target:'b', kind:'outlink', weight:1},
  {source:'a', target:'d', kind:'mention', weight:2},
  {source:'b', target:'d', kind:'mention', weight:3},
  {source:'d', target:'e', kind:'outlink', weight:1},
] };
test('groups confirmed identities, removes self edges, preserves all evidence', () => {
  const g=buildNetwork(raw);
  assert.equal(g.nodes.length,3);
  assert.equal(g.links[0].weight,5);
  assert.equal(g.links[0].evidence.length,2);
  assert.equal(buildNetwork(raw,false).nodes.length,4);
  assert.equal(raw.nodes[0].id,'a');
});
test('former names find creator and immediate neighbors; expansion respects edge filters', () => {
  const g=buildNetwork(raw), kinds=new Set(['mention','outlink']);
  assert.equal(visibleNetwork(g,'old handle',kinds,null).nodes.length,2);
  assert.equal(visibleNetwork(g,'',kinds,'creator:c',2).nodes.length,3);
  assert.equal(visibleNetwork(g,'',new Set(['outlink']),'creator:c').nodes.length,1);
});
test('wiki links addressed to creator:id join the grouped creator', () => {
  const g=buildNetwork({
    nodes:[
      {id:'a',label:'Chan',creatorId:'c',creatorName:'Creator'},
      {id:'d',label:'Neighbor'},
    ],
    links:[{source:'creator:c',target:'d',kind:'wiki',weight:1,evidence:'Ben → Devan',href:'/wiki/creator/ben-avery'}],
  });
  assert.equal(g.links.length,1);
  assert.equal(g.links[0].source,'creator:c');
  assert.equal(g.links[0].target,'d');
  assert.equal(g.links[0].kind,'wiki');
});
test('vault search text finds a creator', () => {
  const g=buildNetwork({
    nodes:[{id:'a',label:'Chan',creatorId:'c',creatorName:'Creator',search:'Devan Costa hate-watch'}],
    links:[],
  });
  assert.equal(visibleNetwork(g,'devan',new Set(['outlink','wiki']),null).nodes.length,1);
});

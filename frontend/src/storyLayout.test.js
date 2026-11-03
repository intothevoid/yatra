import { test } from 'node:test';
import assert from 'node:assert/strict';
import { storyLayout } from './storyLayout.js';

test('saved Koenig paragraphs become headings and related photos become a lead plus four image grid', () => {
  const heading = {type:'paragraph',children:[{type:'extended-text',text:'### Food'}]};
  const paragraph = {type:'paragraph',children:[{type:'extended-text',text:'Lunch by the beach.'}]};
  const images = Array.from({length:5}, (_,i) => ({type:'image',version:1,src:`/api/v1/photos/p_${i}/thumbnail`,caption:''}));
  const state = {root:{children:[heading,paragraph,...images]}};
  const result = storyLayout(state);
  const nodes = result.root.children;
  assert.equal(nodes[0].type,'heading');
  assert.equal(nodes[0].tag,'h3');
  assert.equal(nodes[2].cardWidth,'full');
  assert.deepEqual(nodes[3].images.map(image=>image.row),[0,0,0,1]);
  assert.deepEqual(storyLayout(result),result);
  assert.equal(state.root.children[0].type,'paragraph');
});

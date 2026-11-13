import test from 'node:test';
import assert from 'node:assert/strict';
import { initialLexical, blocksFromLexical } from './articleEditor.js';

test('initialLexical creates a silent looping video card with its poster', () => {
  const article = { blocks: [{ id: 'before', type: 'paragraph', text: 'Before' }, { id: 'clip', type: 'video', video_id: 'v_clip' }] };
  const video = { id: 'v_clip', file_name: 'coast.mov', url: '/api/v1/videos/v_clip/content', poster_url: '/api/v1/videos/v_clip/poster', width: 1280, height: 720, duration: 6 };
  const state = initialLexical(article, [], [video]);
  const node = state.root.children.find((child) => child.type === 'video');
  assert.ok(node);
  assert.equal(node.src, video.url);
  assert.equal(node.customThumbnailSrc, video.poster_url);
  assert.equal(node.loop, true);
  assert.equal(node.muted, true);
  assert.equal(node.autoplay, true);
  assert.equal(node.showControls, false);
});

test('video blocks survive editor serialization in their document position', () => {
  const state = { root: { children: [
    { type: 'paragraph', children: [{ text: 'Before' }] },
    { type: 'video', src: '/api/v1/videos/v_clip/content', fileName: 'v_clip', caption: '' },
    { type: 'paragraph', children: [{ text: 'After' }] },
  ] } };
  const blocks = blocksFromLexical(state, []);
  assert.deepEqual(blocks.map((block) => block.type), ['paragraph', 'video', 'paragraph']);
  assert.equal(blocks[1].video_id, 'v_clip');
});

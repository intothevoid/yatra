import { storyLayout } from './storyLayout.js';

function blockToNode(block) {
  if (block.type === 'heading') {
    return { type: 'heading', tag: `h${Math.min(6, Math.max(1, block.level || 2))}`, format: '', indent: 0, version: 1, direction: null, children: [{ type: 'text', text: block.text || '', format: 0, detail: 0, mode: 'normal', style: '', version: 1 }] };
  }
  return { type: 'paragraph', format: '', indent: 0, version: 1, direction: null, textFormat: 0, textStyle: '', children: [{ type: 'text', text: block.text || '', format: 0, detail: 0, mode: 'normal', style: '', version: 1 }] };
}

export function initialLexical(article, photos) {
  if (article.lexical) {
    try { return storyLayout(JSON.parse(article.lexical), photos); } catch { /* use generated blocks */ }
  }
  const photoByID = new Map((photos || []).map((photo) => [photo.id, photo]));
  const children = [];
  for (const block of article.blocks || []) {
    if (block.type === 'heading' || block.type === 'paragraph') children.push(blockToNode(block));
    if (block.type === 'gallery') {
      const images = (block.photo_ids || []).map((id) => photoByID.get(id)).filter(Boolean).map((photo, index) => ({
        row: index < 2 ? 0 : 1,
        src: photo.thumbnail, fileName: photo.id, width: 1200, height: 800, alt: photo.description || '', caption: block.photo_captions?.[index] || photo.description || '',
      }));
      if (images.length) children.push({ type: 'gallery', version: 1, caption: block.caption || '', images });
    }
    if (block.type === 'image' && block.photo_ids?.[0]) {
      const photo = photoByID.get(block.photo_ids[0]);
      if (photo) children.push({ type: 'image', version: 1, src: photo.thumbnail, width: 1200, height: 800, alt: block.alt_text || '', caption: block.caption || '', title: '', cardWidth: block.card_width || 'full', href: '' });
    }
  }
  return storyLayout({ root: { type: 'root', version: 1, format: '', indent: 0, direction: null, children } }, photos);
}

export function blocksFromLexical(state, oldBlocks) {
  const preserved = (oldBlocks || []).filter((b) => b.type === 'gallery' || b.type === 'image');
  const content = [];
  let id = 1;
  for (const node of state?.root?.children || []) {
    if (node.type === 'gallery') {
      const photoIDs = (node.images || []).map((image) => image.src?.match(/\/photos\/([^/]+)\/thumbnail/)?.[1] || (image.fileName?.startsWith('p_') ? image.fileName : null)).filter(Boolean);
      if (photoIDs.length) content.push({ id: `edit_gallery_${id++}`, type: 'gallery', photo_ids: photoIDs, photo_captions: (node.images || []).map((image) => image.caption || ''), caption: node.caption || '' });
      continue;
    }
    if (node.type === 'image') {
      const photoID = node.src?.match(/\/photos\/([^/]+)\/thumbnail/)?.[1];
      if (photoID) content.push({ id: `edit_image_${id++}`, type: 'image', photo_ids: [photoID], alt_text: node.alt || '', caption: node.caption || '', card_width: node.cardWidth || 'full' });
      continue;
    }
    const text = (node.children || []).map((child) => child.text || '').join('');
    if (!text.trim()) continue;
    const heading = (node.type === 'heading' || node.type === 'extended-heading');
    content.push({ id: `edit_${id++}`, type: heading ? 'heading' : 'paragraph', text, ...(heading ? { level: Number(String(node.tag || 'h2').slice(1)) || 2 } : {}) });
  }
  if (!state?.root && preserved.length && content.length) content.splice(1, 0, ...preserved);
  return content;
}


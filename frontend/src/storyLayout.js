const textOf = (node) => node.text ?? (node.children || []).map(textOf).join('');
const cleanCaption = (value) => {
  if (!value) return '';
  const element = document.createElement('div');
  element.innerHTML = value || '';
  // Koenig stores captions as HTML; decode escaped wrappers too.
  if (element.textContent.includes('<span')) element.innerHTML = element.textContent;
  return element.textContent.trim();
};

export function storyLayout(state, photos = []) {
  const descriptions = new Map(photos.map((photo) => [photo.id, photo.description || ""]));
  const imageCaption = (image) => cleanCaption(image.caption || descriptions.get(image.fileName || image.src?.match(/photos\/([^/]+)/)?.[1]) || "");
  const result = structuredClone(state);
  if (!result.root) return result;
  const nodes = [];
  for (const node of result.root.children || []) {
    if (node.type === 'paragraph' || node.type === 'heading' || node.type === 'extended-heading') {
      const text = textOf(node);
      const heading = text.match(/^\s*(#{1,6})\s+([^\n]+)(?:\n([\s\S]*))?$/);
      if (heading) {
        nodes.push({ ...node, type: 'heading', tag: `h${heading[1].length}`, children: [{ type: 'text', version: 1, text: heading[2], format: 0, detail: 0, mode: 'normal', style: '' }] });
        if (heading[3]?.trim()) nodes.push({ ...node, type: 'paragraph', children: [{ type: 'text', version: 1, text: heading[3].trim(), format: 0, detail: 0, mode: 'normal', style: '' }] });
        continue;
      }
    }
    nodes.push(node);
  }
  const output = [];
  const flush = (section) => {
    const prose = [], images = [];
    for (const node of section) {
      if (node.type === 'image') images.push({ ...node, href: node.href || '', caption: imageCaption(node) });
      else if (node.type === 'gallery') images.push(...(node.images || []).map((image) => ({ ...image, type: 'image', version: 1, caption: imageCaption(image), href: '' })));
      else prose.push(node);
    }
    const cards = [];
    while (images.length) {
      const count = cards.length === 0 && images.length !== 2 ? 1 : images.length >= 4 ? 4 : Math.min(2, images.length);
      const group = images.splice(0, count);
      if (count === 1) cards.push({ ...group[0], cardWidth: 'full' });
      else cards.push({ type: 'gallery', version: 1, images: group.map((image, index) => ({ ...image, fileName: image.fileName || image.src?.match(/photos\/([^/]+)/)?.[1] || `photo-${index}.jpg`, width: image.width || 1200, height: image.height || 800, row: Math.floor(index / 3) })), caption: group.map((image) => image.caption).filter(Boolean).join(' · ') });
    }
    let inserted = false;
    for (const node of prose) {
      output.push(node);
      if (!inserted && node.type === 'paragraph' && cards.length) { output.push(cards.shift()); inserted = true; }
    }
    output.push(...cards);
  };
  let section = [];
  for (const node of nodes) {
    if ((node.type === 'heading' || node.type === 'extended-heading') && section.length) { flush(section); section = []; }
    section.push(node);
  }
  flush(section);
  result.root.children = output;
  return result;
}

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { initialLexical, blocksFromLexical } from '../src/articleEditor.js';
import { storyLayout } from '../src/storyLayout.js';
const require = createRequire(import.meta.url);
const { JSDOM } = require('jsdom');
const { LexicalHTMLRenderer } = require('@tryghost/kg-lexical-html-renderer');
const { htmlToLexical } = require('@tryghost/kg-html-to-lexical');
const { DEFAULT_NODES } = require('@tryghost/kg-default-nodes');
const { createHeadlessEditor } = require('@lexical/headless');
globalThis.document = new JSDOM().window.document;
const renderer = new LexicalHTMLRenderer({nodes:DEFAULT_NODES,onError(error) { throw error; }});
const paragraph = text => ({type:'paragraph',text});
const photo = id => ({id,thumbnail:`/api/v1/photos/${id}/thumbnail`,description:`Description for ${id}`});
const fixturePhotos = Array.from({length:11},(_,i)=>photo(`p_${i}`));
const fixture = {
  feature_photo_id:'p_0',
  blocks:[
    {type:'heading',level:2,text:'Grazing Through the Markets'},paragraph('Cheese, coffee and fresh produce at the market.'),
    ...fixturePhotos.slice(1,6).map(p=>({type:'image',photo_ids:[p.id],caption:p.description})),
    {type:'heading',level:2,text:'Busan Baby Dinner'},paragraph('A Korean dinner surrounded by neon signs.'),
    ...fixturePhotos.slice(6).map(p=>({type:'image',photo_ids:[p.id],caption:p.description})),
  ]
};
function editorRoundTrip(state) {
  const editor = createHeadlessEditor({nodes:DEFAULT_NODES,onError(error) {throw error;}});
  return editor.parseEditorState(state).toJSON();
}
function exportThroughGo(article) {
  const dir=mkdtempSync(join(tmpdir(),'yatra-ghost-render-'));
  try {
    const input=join(dir,'article.json'),output=join(dir,'post.json');
    writeFileSync(input,JSON.stringify(article),{mode:0o600});
    const result=spawnSync('go',['test','./internal/services','-run','^TestGhostRenderContract$','-count=1'],{
      cwd:resolve(import.meta.dirname,'../..'),encoding:'utf8',
      env:{...process.env,YATRA_GHOST_RENDER_INPUT:input,YATRA_GHOST_RENDER_OUTPUT:output}
    });
    assert.equal(result.status,0,result.stdout+result.stderr);
    return JSON.parse(readFileSync(output));
  } finally {rmSync(dir,{recursive:true,force:true});}
}
function assertRendering(html,post,photos,headingTitles) {
  const doc=new JSDOM(html).window.document;
  const expected=new Set(photos.map(p=>`https://example.test/content/images/${p.id}.jpg`));
  const actual=[post.feature_image,...Array.from(doc.querySelectorAll('img'),n=>n.getAttribute('src'))];
  assert.equal(actual.length,expected.size,'photo count changed during Ghost rendering');
  assert.deepEqual(new Set(actual),expected,'a selected photo was lost or duplicated');
  for (const title of headingTitles) assert.ok([...doc.querySelectorAll('h2,h3')].some(h=>h.textContent===title),`missing heading: ${title}`);
  assert.equal(doc.body.textContent.includes('<span'),false,'caption contains literal HTML');
  const galleries=[...doc.querySelectorAll('.kg-gallery-card')];
  assert.ok(galleries.length>=2,'expected photo groups');
  for(const gallery of galleries) {
    const rows=[...gallery.querySelectorAll('.kg-gallery-row')];
    assert.deepEqual(rows.map(r=>r.querySelectorAll('img').length),[2,2],'four photo group must render as two rows of two');
  }
  assert.ok(doc.querySelector('.kg-width-full'),'expected a full width photo');
  const children=[...doc.body.children];
  const firstHeading=children.findIndex(n=>n.textContent==='Grazing Through the Markets');
  const dinnerHeading=children.findIndex(n=>n.textContent==='Busan Baby Dinner');
  const marketImageURLs=children.slice(firstHeading,dinnerHeading).flatMap(n=>[...n.querySelectorAll('img')].map(img=>img.src));
  assert.equal(marketImageURLs.length,5,'market photos moved away from market text');
}

test('11 photos and real Koenig headings survive editor save, Go export, and BOTH Ghost rendering paths',async()=>{
  const state=editorRoundTrip(initialLexical(fixture,fixturePhotos));
  assert.ok(state.root.children.some(n=>n.type==='extended-heading'),'fixture must exercise actual editor heading types');
  const blocks=blocksFromLexical(state,fixture.blocks);
  assert.equal(blocks.filter(b=>b.type==='heading').length,2,'saving converted editor headings to paragraphs');
  const article={...fixture,blocks,lexical:JSON.stringify(state)};
  const post=exportThroughGo(article);
  const lexicalHTML=await renderer.render(post.lexical);
  const fallbackState=await htmlToLexical(post.html);
  const fallbackHTML=await renderer.render(fallbackState);
  for(const html of [lexicalHTML,fallbackHTML]) assertRendering(html,post,fixturePhotos,['Grazing Through the Markets','Busan Baby Dinner']);
  // Older Ghost versions accept core node types; editor extensions stay local.
  assert.equal(post.lexical.includes('extended-heading'),false);
  assert.equal(post.lexical.includes('extended-text'),false);
  const reopened=storyLayout(state,fixturePhotos);
  assert.equal(reopened.root.children.filter(n=>n.type==='gallery').length,2);
});

test('test detects the previous missing-dimensions bug after Ghost HTML import',async()=>{
  const broken='<figure class="kg-card kg-gallery-card"><div class="kg-gallery-container"><div class="kg-gallery-row"><div class="kg-gallery-image"><img src="https://example.test/one.jpg"></div><div class="kg-gallery-image"><img src="https://example.test/two.jpg"></div></div></div></figure>';
  const html=await renderer.render(await htmlToLexical(broken));
  assert.equal(new JSDOM(html).window.document.querySelectorAll('img').length,0,'the regression test must reproduce Ghost dropping malformed galleries');
});

// Optional browser pass: use the installed Chrome, keeping normal CI checks
// independent of browser downloads. The generated report contains synthetic data.
test('desktop and phone preview: all 11 images are visible, grids align, and headings are larger than prose',
  {skip:process.env.YATRA_GHOST_BROWSER !== '1'},async()=>{
    const {chromium}=await import('playwright-core');
    const state=editorRoundTrip(initialLexical(fixture,fixturePhotos));
    const article={...fixture,blocks:blocksFromLexical(state,fixture.blocks),lexical:JSON.stringify(state)};
    const post=exportThroughGo(article);
    const html=await renderer.render(await htmlToLexical(post.html));
    const output=resolve(import.meta.dirname,'../test-results/ghost-rendering');
    const {mkdirSync}=await import('node:fs');mkdirSync(output,{recursive:true});
    let css=`body{margin:0;font-family:Arial,sans-serif} .gh-content{max-width:1100px;margin:40px auto;padding:0 20px} p{font-size:18px}h2{font-size:30px}figure{margin:30px 0}img{display:block;max-width:100%;height:auto} .kg-gallery-row{display:flex;gap:12px;margin-top:12px}.kg-gallery-image{flex:1;min-width:0}figcaption{font-size:14px;margin:10px 0}`;
    if(process.env.YATRA_GHOST_SCREEN_CSS && process.env.YATRA_GHOST_CARDS_CSS) css=readFileSync(process.env.YATRA_GHOST_SCREEN_CSS,'utf8')+'\n'+readFileSync(process.env.YATRA_GHOST_CARDS_CSS,'utf8');
    let pageHTML=`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Ghost render regression preview</title><style>${css}</style></head><body class="post-template"><main class="site-main"><article class="gh-article gh-outer"><header class="gh-article-header gh-canvas"><h1 class="gh-article-title">Market and Dinner — local test</h1></header><div class="gh-content gh-canvas"><figure class="kg-card kg-image-card kg-width-full"><img src="${post.feature_image}" width="1600" height="1200" alt="Header"></figure>${html}</div></article></main></body></html>`;
    // Embed labeled placeholders so the preview never uploads or downloads photos.
    for(const photo of fixturePhotos) {
      const svg=`<svg xmlns="http://www.w3.org/2000/svg" width="1600" height="1200"><rect width="1600" height="1200" fill="${Number(photo.id.slice(2))<6?'#b7d0bd':'#e9c2ac'}"/><text x="800" y="600" font-family="Arial" font-size="85" text-anchor="middle">${photo.id} — ${Number(photo.id.slice(2))<6?'Market':'Dinner'}</text></svg>`;
      pageHTML=pageHTML.replaceAll(`https://example.test/content/images/${photo.id}.jpg`,'data:image/svg+xml;base64,'+Buffer.from(svg).toString('base64'));
    }
    const preview=join(output,'preview.html');writeFileSync(preview,pageHTML);
    const browser=await chromium.launch({channel:process.env.YATRA_CHROME_CHANNEL || 'chrome',headless:true});
    try {
      for(const [name,width,height] of [['desktop',1440,1000],['phone',390,844]]) {
        const page=await browser.newPage({viewport:{width,height}});
        await page.route('https://**/*',route=>route.abort());
        await page.goto(new URL(`file://${preview}`).href);
        await page.locator('img').evaluateAll(images=>Promise.all(images.map(image=>image.decode())));
        const metrics=await page.evaluate(()=>({
          images:[...document.images].map(img=>({loaded:img.complete&&img.naturalWidth>0,width:img.getBoundingClientRect().width,height:img.getBoundingClientRect().height})),
          headingSize:parseFloat(getComputedStyle(document.querySelector('.gh-content h2')).fontSize),
          proseSize:parseFloat(getComputedStyle(document.querySelector('.gh-content p')).fontSize),
          overflow:document.documentElement.scrollWidth>innerWidth+1,
          rows:[...document.querySelectorAll('.kg-gallery-row')].map(row=>[...row.querySelectorAll('img')].map(img=>({x:img.getBoundingClientRect().x,y:img.getBoundingClientRect().y})))
        }));
        await page.screenshot({path:join(output,`${name}.png`),fullPage:true});
        assert.equal(metrics.images.length,11);
        assert.ok(metrics.images.every(img=>img.loaded&&img.width>20&&img.height>20),`${name}: hidden or broken photo`);
        assert.ok(metrics.headingSize>metrics.proseSize,`${name}: heading looks like paragraph`);
        assert.equal(metrics.overflow,false,`${name}: horizontal overflow`);
        for(const row of metrics.rows) {assert.equal(row.length,2);assert.ok(row[1].x>row[0].x);assert.ok(Math.abs(row[1].y-row[0].y)<2);}

        await page.close();
      }
    } finally {await browser.close();}
  });

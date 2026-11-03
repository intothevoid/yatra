import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';

const WritingEditor = React.lazy(() => import('./WritingEditor.jsx'));

const api = async (path, options = {}) => {
  const response = await fetch(path, options);
  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    try { message = (await response.json()).message || message; } catch { /* keep fallback */ }
    throw new Error(message);
  }
  if (response.status === 204) return null;
  return response.json();
};

function blockToNode(block) {
  if (block.type === 'heading') {
    return { type: 'heading', tag: `h${Math.min(6, Math.max(1, block.level || 2))}`, format: '', indent: 0, version: 1, direction: null, children: [{ type: 'text', text: block.text || '', format: 0, detail: 0, mode: 'normal', style: '', version: 1 }] };
  }
  return { type: 'paragraph', format: '', indent: 0, version: 1, direction: null, textFormat: 0, textStyle: '', children: [{ type: 'text', text: block.text || '', format: 0, detail: 0, mode: 'normal', style: '', version: 1 }] };
}

function initialLexical(article, photos) {
  if (article.lexical) {
    try { return JSON.parse(article.lexical); } catch { /* use generated blocks */ }
  }
  const photoByID = new Map((photos || []).map((photo) => [photo.id, photo]));
  const children = [];
  for (const block of article.blocks || []) {
    if (block.type === 'heading' || block.type === 'paragraph') children.push(blockToNode(block));
    if (block.type === 'gallery') {
      const images = (block.photo_ids || []).map((id) => photoByID.get(id)).filter(Boolean).map((photo) => ({
        src: photo.thumbnail, fileName: photo.id, width: 1200, height: 800, alt: photo.description || '', caption: '',
      }));
      if (images.length) children.push({ type: 'gallery', version: 1, caption: block.caption || '', images });
    }
    if (block.type === 'image' && block.photo_ids?.[0]) {
      const photo = photoByID.get(block.photo_ids[0]);
      if (photo) children.push({ type: 'image', version: 1, src: photo.thumbnail, width: 1200, height: 800, alt: block.alt_text || '', caption: block.caption || '', title: '', cardWidth: 'regular', href: '' });
    }
  }
  return { root: { type: 'root', version: 1, format: '', indent: 0, direction: null, children } };
}

function blocksFromLexical(state, oldBlocks) {
  const preserved = (oldBlocks || []).filter((b) => b.type === 'gallery' || b.type === 'image');
  const content = [];
  const galleries = [];
  let id = 1;
  for (const node of state?.root?.children || []) {
    if (node.type === 'gallery') {
      const photoIDs = (node.images || []).map((image) => image.src?.match(/\/photos\/([^/]+)\/thumbnail/)?.[1] || (image.fileName?.startsWith('p_') ? image.fileName : null)).filter(Boolean);
      if (photoIDs.length) galleries.push({ id: `edit_gallery_${id++}`, type: 'gallery', photo_ids: photoIDs, caption: node.caption || '' });
      continue;
    }
    if (node.type === 'image') {
      const photoID = node.src?.match(/\/photos\/([^/]+)\/thumbnail/)?.[1];
      if (photoID) content.push({ id: `edit_image_${id++}`, type: 'image', photo_ids: [photoID], alt_text: node.alt || '', caption: node.caption || '' });
      continue;
    }
    const text = (node.children || []).map((child) => child.text || '').join('');
    if (!text.trim()) continue;
    const heading = node.type === 'heading';
    content.push({ id: `edit_${id++}`, type: heading ? 'heading' : 'paragraph', text, ...(heading ? { level: Number(String(node.tag || 'h2').slice(1)) || 2 } : {}) });
  }
  if (galleries.length && content.length) content.splice(1, 0, ...galleries);
  else if (!state?.root && preserved.length && content.length) content.splice(1, 0, ...preserved);
  return content;
}

function App() {
  const [trips, setTrips] = useState([]);
  const [trip, setTrip] = useState(null);
  const [photos, setPhotos] = useState([]);
  const [article, setArticle] = useState(null);
  const [screen, setScreen] = useState('home');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [selectedFiles, setSelectedFiles] = useState([]);
  const [maxUploadFiles, setMaxUploadFiles] = useState(200);
  const [photoFilter, setPhotoFilter] = useState('all');
  const [featurePhotoID, setFeaturePhotoID] = useState('');
  const [form, setForm] = useState({ title: '', location: '', notes: '' });
  const fileRef = useRef(null);
  const saveTimer = useRef(null);

  const loadTrips = useCallback(async () => {
    setTrips((await api('/api/v1/trips')) || []);
  }, []);

  const deleteTrip = async (event, item) => {
    event.stopPropagation();
    if (!window.confirm(`Delete “${item.name}” and its saved photos and draft from this app? This will not delete a draft already sent to Ghost.`)) return;
    try {
      await api(`/api/v1/trips/${encodeURIComponent(item.id)}`, { method: 'DELETE' });
      setTrips((items) => items.filter((tripItem) => tripItem.id !== item.id));
      if (trip?.id === item.id) {
        setTrip(null); setPhotos([]); setArticle(null); setScreen('home');
      }
    } catch (e) { setError(e.message); }
  };

  const editorFileUploader = useMemo(() => ({
    fileTypes: { image: { mimeTypes: ['image/jpeg', 'image/png', 'image/heic', 'image/webp'], extensions: ['jpg', 'jpeg', 'png', 'heic', 'heif', 'webp'] } },
    useFileUpload: (type) => ({
      isLoading: false, progress: 0, errors: [], filesNumber: 0,
      upload: async (files) => {
        if (type !== 'image' || !trip?.id) return null;
        setNotice('Adding image to your trip…');
        try {
          const data = new FormData();
          Array.from(files).forEach((file) => data.append('files', file, file.name));
          const result = await api(`/api/v1/trips/${trip.id}/photos/upload`, { method: 'POST', body: data });
          setPhotos(await api(`/api/v1/trips/${trip.id}/photos`));
          setNotice('');
          return result.uploads;
        } catch (e) { setError(e.message); setNotice(''); return null; }
      },
    }),
  }), [trip?.id]);

  const waitForJob = (jobID, label) => new Promise((resolve, reject) => {
    const stream = new EventSource(`/api/v1/jobs/${jobID}/progress`);
    stream.addEventListener('progress', (event) => {
      try {
        const status = JSON.parse(event.data);
        setNotice(status.progress ? `${label} · ${status.progress}%` : (status.step || label));
      } catch { /* ignore malformed progress */ }
    });
    stream.addEventListener('complete', (event) => {
      stream.close();
      try {
        const status = JSON.parse(event.data);
        if (status.state !== 'succeeded') reject(new Error(status.error || `${label} failed.`));
        else resolve(status);
      } catch (e) { reject(e); }
    });
    stream.onerror = () => { stream.close(); reject(new Error('Lost the progress connection. The job may still be running; refresh this trip to check.')); };
  });

  useEffect(() => { loadTrips().catch((e) => setError(e.message)); }, [loadTrips]);
  useEffect(() => { api('/api/v1/settings').then((settings) => { if (settings.max_upload_files > 0) setMaxUploadFiles(settings.max_upload_files); }).catch(() => {}); }, []);
  useEffect(() => () => clearTimeout(saveTimer.current), []);

  const addSelectedFiles = (event) => {
    const incoming = Array.from(event.target.files || []);
    event.target.value = '';
    const remaining = Math.max(0, maxUploadFiles - selectedFiles.length);
    if (incoming.length > remaining) setError(`You can upload up to ${maxUploadFiles} photos at a time.`);
    setSelectedFiles((old) => [...old, ...incoming.slice(0, remaining)]);
  };

  const openTrip = async (id) => {
    setError(''); setNotice(''); setBusy(true);
    try {
      const [nextTrip, nextPhotos] = await Promise.all([api(`/api/v1/trips/${id}`), api(`/api/v1/trips/${id}/photos`)]);
      let existingArticle = null;
      if (['EDITING', 'ARTICLE_APPROVED', 'EXPORTED_DRAFT'].includes(nextTrip.status)) {
        try { existingArticle = await api(`/api/v1/articles/art_${id}`); } catch { /* no saved article yet */ }
      }
      setTrip(nextTrip); setPhotos(nextPhotos || []); setArticle(existingArticle); setPhotoFilter('all'); setFeaturePhotoID(nextTrip.feature_photo_id || ''); setScreen('trip');
      if (!existingArticle && nextPhotos?.length && nextPhotos.some((photo) => !photo.description)) {
        setBusy(true); setNotice('Describing your photos… This may take a few minutes.');
        try {
          const job = await api(`/api/v1/trips/${id}/analyse`, { method: 'POST' });
          await waitForJob(job.job_id, 'Describing photos');
          setPhotos((await api(`/api/v1/trips/${id}/photos`)) || []);
        } catch (e) {
          setError(`Photos are ready to review, but automatic descriptions could not be generated: ${e.message}`);
        } finally { setBusy(false); setNotice(''); }
      }
    } catch (e) { setError(e.message); }
    finally { setBusy(false); }
  };

  const setPhotoSelected = async (photo, selected) => {
    const status = selected ? 'recommended' : 'not_selected';
    try {
      await api(`/api/v1/trips/${trip.id}/photos/${photo.id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ status }) });
      setPhotos((items) => items.map((p) => p.id === photo.id ? { ...p, status } : p));
      if (!selected && featurePhotoID === photo.id) setFeaturePhotoID('');
    } catch (e) { setError(e.message); }
  };

  const chooseFeaturePhoto = async (photo) => {
    setError('');
    try {
      const updatedTrip = await api(`/api/v1/trips/${trip.id}/feature-photo`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ photo_id: photo.id }) });
      setTrip(updatedTrip); setFeaturePhotoID(photo.id);
      setPhotos((items) => items.map((p) => p.id === photo.id ? { ...p, status: ['not_selected', 'unknown'].includes(p.status) ? 'recommended' : p.status } : p));
    } catch (e) { setError(e.message); }
  };

  const uploadTrip = async (event) => {
    event.preventDefault(); setError(''); setBusy(true); setNotice('Uploading and preparing your photos…');
    try {
      const data = new FormData();
      data.append('title', form.title.trim() || form.location.trim() || 'Untitled trip');
      data.append('location', form.location.trim()); data.append('notes', form.notes.trim());
      selectedFiles.forEach((file) => data.append('files', file, file.name));
      const created = await api('/api/v1/trips/import-upload', { method: 'POST', body: data });
      setSelectedFiles([]); setForm({ title: '', location: '', notes: '' });
      await loadTrips(); await openTrip(created.trip_id);
    } catch (e) { setError(e.message); }
    finally { setBusy(false); setNotice(''); }
  };

  const generate = async () => {
    if (!trip || !photos.some((p) => p.status !== 'not_selected')) {
      setError('Select at least one photo to create the draft.'); return;
    }
    setError(''); setBusy(true); setNotice('Writing your first draft… This can take a little while.');
    try {
      const job = await api(`/api/v1/trips/${trip.id}/generate`, { method: 'POST' });
      await waitForJob(job.job_id, 'Writing your draft');
      const doc = await api(`/api/v1/articles/art_${trip.id}`);
      setArticle(doc); setScreen('editor');
    } catch (e) { setError(e.message); }
    finally { setBusy(false); setNotice(''); }
  };

  const openEditor = async () => {
    setError(''); setBusy(true);
    try { setArticle(await api(`/api/v1/articles/art_${trip.id}`)); setScreen('editor'); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  };

  const saveArticle = (state) => {
    const lexical = JSON.stringify(state.toJSON ? state.toJSON() : state);
    const next = { ...article, lexical, blocks: blocksFromLexical(state.toJSON ? state.toJSON() : state, article.blocks) };
    setArticle(next); setNotice('Saving…'); clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(async () => {
      try {
        await api(`/api/v1/articles/${article.article_id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(next) });
        setNotice('Saved'); setTimeout(() => setNotice(''), 1800);
      } catch (e) { setError(`Could not save draft: ${e.message}`); setNotice(''); }
    }, 850);
  };

  const pushToGhost = async () => {
    setBusy(true); setError(''); setNotice('Sending draft to Ghost…');
    try {
      clearTimeout(saveTimer.current);
      const finalDoc = article.lexical ? article : { ...article, lexical: JSON.stringify(initialLexical(article, photos)) };
      setArticle(finalDoc);
      await api(`/api/v1/articles/${article.article_id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(finalDoc) });
      await api(`/api/v1/trips/${trip.id}/ghost-draft`, { method: 'POST' });
      setNotice('Ghost draft created');
    } catch (e) { setError(e.message); setNotice(''); }
    finally { setBusy(false); }
  };

  const previewFiles = useMemo(() => selectedFiles.map((file) => ({ file, url: URL.createObjectURL(file) })), [selectedFiles]);
  const recommendedPhotos = photos.filter((photo) => photo.status === 'recommended');
  const visiblePhotos = photoFilter === 'recommended' ? recommendedPhotos : photos;
  useEffect(() => () => previewFiles.forEach((item) => URL.revokeObjectURL(item.url)), [previewFiles]);

  return <div className="app-shell">
    <header className="topbar">
      <button className="brand" onClick={() => { setScreen('home'); setTrip(null); setArticle(null); loadTrips(); }} aria-label="Yatra home"><span className="brand-mark">Y</span><span>yatra</span></button>
      <span className="topbar-note">A little help turning trips into stories</span>
    </header>
    <main className="main-content">
      {(error || notice) && <div className={`message ${error ? 'message-error' : ''}`} role={error ? 'alert' : 'status'}>{error || notice}<button onClick={() => { setError(''); if (error) setNotice(''); }} aria-label="Dismiss">×</button></div>}

      {screen === 'home' && <>
        <section className="welcome">
          <p className="eyebrow">YOUR TRAVEL DIARY</p><h1>Make a story<br className="mobile-break" /> from your photos.</h1>
          <p className="intro">Choose a handful of photos, add the details you remember, and get a draft you can edit before it goes anywhere.</p>
        </section>
        <section className="create-card">
          <div className="section-title"><div><p className="eyebrow">NEW STORY</p><h2>Start with a trip</h2></div><span className="step-count">01</span></div>
          <form onSubmit={uploadTrip}>
            <div className="field-row"><label className="field">Post title<input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="A weekend in Goa" /></label><label className="field">Where did you go?<input required value={form.location} onChange={(e) => setForm({ ...form, location: e.target.value })} placeholder="Goa, India" /></label></div>
            <label className="field">What do you remember?<textarea rows="4" value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} placeholder="The train ride, a meal you loved, a place you stumbled upon…" /></label>
            <div className="upload-head"><div><label className="field-label">Choose photos</label><p className="field-help">JPG, PNG or HEIC. Up to {maxUploadFiles} photos per upload.</p></div><span className="photo-count">{selectedFiles.length} of {maxUploadFiles} selected</span></div>
            <input ref={fileRef} className="file-input" type="file" multiple accept="image/*,.heic,.heif" onChange={addSelectedFiles} />
            <button type="button" className="pick-button" disabled={selectedFiles.length >= maxUploadFiles} onClick={() => fileRef.current?.click()}><span className="plus">＋</span><span>{selectedFiles.length ? (selectedFiles.length >= maxUploadFiles ? 'Photo limit reached' : 'Add more photos') : 'Choose photos from this device'}</span></button>
            {previewFiles.length > 0 && <div className="preview-grid">{previewFiles.map(({ file, url }, i) => <button className="local-photo" type="button" key={`${file.name}-${i}`} title={`Remove ${file.name}`} onClick={() => setSelectedFiles((items) => items.filter((_, index) => index !== i))}><img src={url} alt={file.name} /><span>×</span></button>)}</div>}
            <button className="button button-primary full-button" disabled={busy || selectedFiles.length === 0}>{busy ? 'Preparing…' : `Upload ${selectedFiles.length || ''} ${selectedFiles.length === 1 ? 'photo' : 'photos'} and review`}</button>
          </form>
        </section>
        {trips?.length > 0 && <section className="recent-section"><div className="section-title"><div><p className="eyebrow">PICK UP WHERE YOU LEFT OFF</p><h2>Your stories</h2></div></div><div className="trip-list">{trips.map((item) => <div key={item.id} className="trip-row"><button className="trip-open-button" onClick={() => openTrip(item.id)}><span><strong>{item.name}</strong><small>{item.location || 'Travel story'} · {new Date(item.updated_at).toLocaleDateString()}</small></span><span className="trip-status">{item.status.toLowerCase().replaceAll('_', ' ')}</span><span className="arrow">→</span></button><button className="trip-delete-button" title={`Delete ${item.name}`} aria-label={`Delete ${item.name}`} onClick={(event) => deleteTrip(event, item)}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3.5 5.5h13M8 5.5V3.8h4v1.7m-6.5 0 .8 10.7h7.4l.8-10.7M8.2 8.2v5.5m3.6-5.5v5.5" /></svg></button></div>)}</div></section>}
      </>}

      {screen === 'trip' && trip && <>
        <button className="back-link" onClick={() => setScreen('home')}>← All stories</button>
        <section className="trip-heading"><p className="eyebrow">{trip.location || 'YOUR TRIP'}</p><h1>{trip.name}</h1><p className="intro">Choose the photos that tell the story. We’ll use your notes and destination to write a first draft.</p></section>
        {trip.notes && <section className="notes-card"><p className="eyebrow">YOUR NOTES</p><p>{trip.notes}</p></section>}
        {article ? <button className="button button-primary" onClick={openEditor}>Continue editing this draft</button> : <>
          <div className="review-toolbar"><div><h2>Photo review</h2><p>{photos.filter((p) => p.status !== 'not_selected').length} of {photos.length} selected</p><p className="field-help">Choose the photos for your story. Tap the star on a photo to use it as the post header image.</p></div><div className="review-tools"><button className="text-button" onClick={async () => { const next = photos.map((p) => ({ ...p, status: 'recommended' })); setPhotos(next); try { await api(`/api/v1/trips/${trip.id}/photos/select-all`, { method: 'POST' }); } catch (e) { setError(e.message); try { setPhotos(await api(`/api/v1/trips/${trip.id}/photos`)); } catch { /* retain optimistic state if refresh fails */ } } }}>Select all</button></div></div>
          <div className="photo-filters" role="group" aria-label="Filter photos">
            <button type="button" className={photoFilter === 'all' ? 'active' : ''} onClick={() => setPhotoFilter('all')}>All photos ({photos.length})</button>
            <button type="button" className={photoFilter === 'recommended' ? 'active' : ''} onClick={() => setPhotoFilter('recommended')}>Recommended ({recommendedPhotos.length})</button>
          </div>
          <div className="photo-grid">{visiblePhotos.map((photo) => { const chosen = photo.status !== 'not_selected'; const isFeature = featurePhotoID === photo.id; return <div key={photo.id} className="photo-tile"><button className={`photo-card ${chosen ? 'chosen' : ''}`} onClick={() => setPhotoSelected(photo, !chosen)} aria-pressed={chosen} title={photo.description || undefined}><span className="photo-image"><img src={photo.thumbnail} alt={photo.description || 'Trip photo'} loading="lazy" /><span className="photo-check">{chosen ? '✓' : '+'}</span></span>{photo.description && <span className="photo-description">{photo.description}</span>}</button><button className={`photo-feature-button ${isFeature ? 'active' : ''}`} onClick={() => chooseFeaturePhoto(photo)} aria-label={isFeature ? 'Header image selected' : 'Use as post header image'} aria-pressed={isFeature} title={isFeature ? 'Header image' : 'Use as post header image'}>{isFeature ? '★' : '☆'}</button></div>; })}</div>
          <button className="button button-primary full-button draft-button" disabled={busy || photos.length === 0} onClick={generate}>{busy ? 'Writing your draft…' : 'Create my draft'}<span>→</span></button>
        </>}
      </>}

      {screen === 'editor' && article && <>
        <button className="back-link" onClick={() => setScreen('trip')}>← Photo review</button>
        <div className="editor-head"><div><p className="eyebrow">DRAFT · {trip?.location}</p><h1>Make it yours</h1><p className="intro">Edit the story below. Your changes save automatically.</p></div><div className="editor-actions"><span className="save-status">{notice}</span><button className="button button-ghost" onClick={pushToGhost} disabled={busy}>Send draft to Ghost</button></div></div>
        {article.research_summary && <details className="research-card"><summary>Place background used in this draft</summary><p>{article.research_summary}</p>{article.research_sources?.map((source) => <a key={source} href={source} target="_blank" rel="noreferrer">{new URL(source).hostname}</a>)}</details>}
        <article className="article-editor"><label className="field title-field">Post title<input value={article.title} onChange={(e) => { const next = { ...article, title: e.target.value, slug: e.target.value.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') }; setArticle(next); clearTimeout(saveTimer.current); saveTimer.current = setTimeout(() => api(`/api/v1/articles/${article.article_id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(next) }).then(() => setNotice('Saved')).catch((err) => setError(err.message)), 850); }} /></label>
          <React.Suspense fallback={<div className="editor-loading">Opening the editor…</div>}>
            <WritingEditor key={article.article_id} initialEditorState={initialLexical(article, photos)} fileUploader={editorFileUploader} onChange={saveArticle} />
          </React.Suspense>
        </article>
      </>}
    </main>
    <footer className="footer">Made for the stories behind the photos.</footer>
  </div>;
}

createRoot(document.getElementById('root')).render(<React.StrictMode><App /></React.StrictMode>);

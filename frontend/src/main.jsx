import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';
import { initialLexical, blocksFromLexical } from './articleEditor.js';

const WritingEditor = React.lazy(() => import('./WritingEditor.jsx'));

const api = async (path, options = {}) => {
  const response = await fetch(path, options);
  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    try { const body = await response.text(); try { const parsed = JSON.parse(body); message = parsed.message || parsed.error || message; } catch { message = body.trim() || message; } } catch { /* keep fallback */ }
    throw new Error(message);
  }
  if (response.status === 204) return null;
  return response.json();
};

const isVideoFile = (file) => file.type.startsWith('video/') || /\.(mp4|m4v|mov|webm|mkv|avi|mpeg|mpg|3gp|mts|m2ts|wmv|flv|ogv)$/i.test(file.name);
const trimPreviewProps = (start, end, onMetadata) => ({
  onLoadedMetadata: (event) => { onMetadata(event.currentTarget.duration); event.currentTarget.currentTime = start; },
  onPlay: (event) => { if (event.currentTarget.currentTime < start || event.currentTarget.currentTime >= end) event.currentTarget.currentTime = start; },
  onTimeUpdate: (event) => { if (end > start && event.currentTarget.currentTime >= end) { event.currentTarget.pause(); event.currentTarget.currentTime = end; } },
});


function App() {
  const [authInfo, setAuthInfo] = useState(null);
  const [authPassword, setAuthPassword] = useState('');
  const [authError, setAuthError] = useState('');
  const [authBusy, setAuthBusy] = useState(false);
  const [settings, setSettings] = useState(null);
  const [settingsBusy, setSettingsBusy] = useState(false);
  const [trips, setTrips] = useState([]);
  const [trip, setTrip] = useState(null);
  const [photos, setPhotos] = useState([]);
  const [videos, setVideos] = useState([]);
  const [videoDurations, setVideoDurations] = useState({});
  const [videoTrims, setVideoTrims] = useState({});
  const [videoProfile, setVideoProfile] = useState('standard');
  const [videoDefaultDuration, setVideoDefaultDuration] = useState(6);
  const [videoProcessingID, setVideoProcessingID] = useState('');
  const [videoEditSettings, setVideoEditSettings] = useState({});
  const [tripMediaFiles, setTripMediaFiles] = useState([]);
  const [tripMediaDurations, setTripMediaDurations] = useState({});
  const [tripMediaTrims, setTripMediaTrims] = useState({});
  const [article, setArticle] = useState(null);
  const [screen, setScreen] = useState('home');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [activeJobID, setActiveJobID] = useState('');
  const [stopPending, setStopPending] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState([]);
  const [maxUploadFiles, setMaxUploadFiles] = useState(200);
  const [modelProvider, setModelProvider] = useState('fake');
  const [localConfigured, setLocalConfigured] = useState(false);
  const [openRouterConfigured, setOpenRouterConfigured] = useState(false);
  const [localEndpoint, setLocalEndpoint] = useState('');
  const [localModel, setLocalModel] = useState('');
  const [localKey, setLocalKey] = useState('');
  const [visionEndpoint, setVisionEndpoint] = useState('');
  const [visionModel, setVisionModel] = useState('');
  const [visionKey, setVisionKey] = useState('');
  const [visionConfigured, setVisionConfigured] = useState(false);
  const [openRouterModel, setOpenRouterModel] = useState('');
  const [openRouterKey, setOpenRouterKey] = useState('');
  const [modelTimeoutSeconds, setModelTimeoutSeconds] = useState('');
  const [timeoutSource, setTimeoutSource] = useState('unset');
  const [photoFilter, setPhotoFilter] = useState('all');
  const [featurePhotoID, setFeaturePhotoID] = useState('');
  const [form, setForm] = useState({ title: '', location: '', notes: '' });
  const fileRef = useRef(null);
  const tripMediaRef = useRef(null);
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
        if (status.step?.includes('generated') || status.step?.includes('waiting for the model')) setNotice(status.step);
        else setNotice(status.progress ? `${label} · ${status.progress}%${status.step ? ` · ${status.step}` : ''}` : (status.step || label));
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

  const refreshAuth = useCallback(async () => setAuthInfo(await api('/api/v1/auth/status')), []);
  const loadSettings = async () => {
    const value = await api('/api/v1/app-settings');
    setSettings(value);
    return value;
  };
  const saveSettings = async (event) => {
    event.preventDefault(); setSettingsBusy(true); setError(''); setNotice('Saving settings…');
    try {
      const saved = await api('/api/v1/app-settings', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settings) });
      setSettings(saved); if (saved?.max_upload_files > 0) setMaxUploadFiles(saved.max_upload_files); setNotice('Settings saved and active'); setTimeout(() => setNotice(''), 2400);
    } catch (e) { setError(e.message); setNotice(''); }
    finally { setSettingsBusy(false); }
  };
  const submitAuth = async (event) => {
    event.preventDefault(); setAuthBusy(true); setAuthError('');
    try { await api(`/api/v1/auth/${authInfo?.configured ? 'login' : 'setup'}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: authPassword }) }); setAuthPassword(''); await refreshAuth(); await loadTrips(); }
    catch (e) { setAuthError(e.message); }
    finally { setAuthBusy(false); }
  };
  useEffect(() => { refreshAuth().catch(() => setAuthError('Could not connect to Yatra.')); }, [refreshAuth]);
  useEffect(() => {
    if (!authInfo?.authenticated) return;
    loadTrips().catch((e) => setError(e.message));
    loadSettings().then((value) => { if (value?.max_upload_files > 0) setMaxUploadFiles(value.max_upload_files); }).catch((e) => setError(e.message));
    api('/api/v1/settings').then((value) => {
      if (value.max_upload_files > 0) setMaxUploadFiles(value.max_upload_files);
      if (value.video_default_profile) setVideoProfile(value.video_default_profile);
      if (value.video_default_duration_seconds > 0) setVideoDefaultDuration(value.video_default_duration_seconds);
      setModelProvider(value.model_provider || 'fake'); setLocalConfigured(Boolean(value.local_configured));
      setOpenRouterConfigured(Boolean(value.openrouter_configured)); setLocalEndpoint(value.local_endpoint || '');
      setLocalModel(value.local_model || ''); setVisionEndpoint(value.vision_endpoint || '');
      setVisionModel(value.vision_model || ''); setVisionConfigured(Boolean(value.vision_configured));
      setOpenRouterModel(value.openrouter_model || (value.model_provider === 'openrouter' ? value.model : ''));
      setModelTimeoutSeconds(String(value.model_timeout_sec ?? '')); setTimeoutSource(value.timeout_source || 'unset');
    }).catch((e) => setError(e.message));
  }, [authInfo?.authenticated, loadTrips]);
  useEffect(() => () => clearTimeout(saveTimer.current), []);

  const addSelectedFiles = (event) => {
    const incoming = Array.from(event.target.files || []);
    event.target.value = '';
    const remaining = Math.max(0, maxUploadFiles - selectedFiles.length);
    if (incoming.length > remaining) setError(`You can upload up to ${maxUploadFiles} files at a time.`);
    setSelectedFiles((old) => [...old, ...incoming.slice(0, remaining)]);
  };

  const openTrip = async (id) => {
    setError(''); setNotice(''); setBusy(true);
    try {
      const [nextTrip, nextPhotos, nextVideos] = await Promise.all([api(`/api/v1/trips/${id}`), api(`/api/v1/trips/${id}/photos`), api(`/api/v1/trips/${id}/videos`)]);
      let existingArticle = null;
      if (['EDITING', 'ARTICLE_APPROVED', 'EXPORTED_DRAFT'].includes(nextTrip.status)) {
        try { existingArticle = await api(`/api/v1/articles/art_${id}`); } catch { /* no saved article yet */ }
      }
      setTrip(nextTrip); setPhotos(nextPhotos || []); setVideos(nextVideos || []); setArticle(existingArticle); setPhotoFilter('all'); setFeaturePhotoID(nextTrip.feature_photo_id || ''); setScreen('trip');
      setVideoEditSettings(Object.fromEntries((nextVideos||[]).map((v)=>[v.id,{start:v.start_time||0,end:v.end_time||Math.min(videoDefaultDuration,v.source_duration||v.duration),profile:v.profile||'standard'}])));
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

  const setVideoSelected = async (video, selected) => {
    try { await api(`/api/v1/trips/${trip.id}/videos/${video.id}`, { method:'PATCH', headers:{'Content-Type':'application/json'}, body:JSON.stringify({selected}) }); setVideos((items)=>items.map((v)=>v.id===video.id?{...v,selected}:v)); }
    catch(e){setError(e.message);}
  };

  const reprocessVideo = async (video) => {
    const settings=videoEditSettings[video.id]||{};setVideoProcessingID(video.id);setError('');setNotice(`Processing ${video.file_name}…`);
    try{await api(`/api/v1/trips/${trip.id}/videos/${video.id}/process`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({start_time:Number(settings.start||0),end_time:Number(settings.end||0),profile:settings.profile||'standard'})});const updated=await api(`/api/v1/trips/${trip.id}/videos`);setVideos(updated||[]);setNotice('Video processed');setTimeout(()=>setNotice(''),1800);}
    catch(e){setError(e.message);setNotice('');}
    finally{setVideoProcessingID('');}
  };

  const addTripMedia = (event)=>{const added=Array.from(event.target.files||[]);event.target.value='';setTripMediaFiles((current)=>[...current,...added]);};
  const uploadTripMedia = async()=>{
    const bad=tripMediaFiles.find((file)=>isVideoFile(file)&&(tripMediaDurations[file.name]==null||(tripMediaTrims[file.name]?.end??0)<=(tripMediaTrims[file.name]?.start??0)||(tripMediaTrims[file.name]?.end??0)>tripMediaDurations[file.name]));
    if(bad){setError(`Choose a valid trim range for ${bad.name}.`);return;}
    setBusy(true);setNotice('Adding media to this trip…');setError('');
    try{const data=new FormData();data.append('video_profile',videoProfile);tripMediaFiles.forEach((file)=>{data.append('files',file,file.name);if(isVideoFile(file)){const trim=tripMediaTrims[file.name]||{};data.append(`video_trim_${file.name}_start`,String(trim.start??0));data.append(`video_trim_${file.name}_end`,String(trim.end??tripMediaDurations[file.name]));}});await api(`/api/v1/trips/${trip.id}/photos/upload`,{method:'POST',body:data});const [nextPhotos,nextVideos]=await Promise.all([api(`/api/v1/trips/${trip.id}/photos`),api(`/api/v1/trips/${trip.id}/videos`)]);setPhotos(nextPhotos||[]);setVideos(nextVideos||[]);setTripMediaFiles([]);setTripMediaTrims({});setTripMediaDurations({});setNotice('Media added');setTimeout(()=>setNotice(''),1800);}
    catch(e){setError(e.message);setNotice('');}finally{setBusy(false);}
  };

  const recordVideoDuration = (file, duration) => {
    if (!Number.isFinite(duration) || duration <= 0) return;
    setVideoDurations((current) => ({ ...current, [file.name]: duration }));
    setVideoTrims((current) => current[file.name] ? current : ({ ...current, [file.name]: { start: 0, end: Math.min(videoDefaultDuration, duration) } }));
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
    event.preventDefault(); setError('');
    const badVideo = selectedFiles.find((file)=>isVideoFile(file) && (videoDurations[file.name] == null || (videoTrims[file.name]?.start ?? -1) < 0 || (videoTrims[file.name]?.end ?? 0) <= (videoTrims[file.name]?.start ?? 0) || (videoTrims[file.name]?.end ?? 0) > videoDurations[file.name]));
    if (badVideo) { setError(`Choose a valid start and end range within the duration of ${badVideo.name}.`); return; }
    setBusy(true); setNotice('Uploading and preparing your photos and videos…');
    try {
      const data = new FormData();
      data.append('title', form.title.trim() || form.location.trim() || 'Untitled trip');
      data.append('location', form.location.trim()); data.append('notes', form.notes.trim());
      data.append('video_profile', videoProfile);
      selectedFiles.forEach((file) => data.append('files', file, file.name));
      selectedFiles.filter(isVideoFile).forEach((file) => { const trim=videoTrims[file.name]||{}; data.append(`video_trim_${file.name}_start`, String(trim.start ?? 0)); data.append(`video_trim_${file.name}_end`, String(trim.end ?? videoDurations[file.name])); });
      const created = await api('/api/v1/trips/import-upload', { method: 'POST', body: data });
      setSelectedFiles([]); setVideoTrims({}); setVideoDurations({}); setForm({ title: '', location: '', notes: '' });
      await loadTrips(); await openTrip(created.trip_id);
    } catch (e) { setError(e.message); }
    finally { setBusy(false); setNotice(''); }
  };

  const generate = async () => {
    if (!trip || (!photos.some((p) => p.status !== 'not_selected') && !videos.some((v)=>v.selected))) {
      setError('Select at least one photo or video to create the draft.'); return;
    }
    setError(''); setBusy(true); setNotice('Writing your first draft… This can take a little while.');
    try {
      const job = await api(`/api/v1/trips/${trip.id}/generate`, { method: 'POST' });
      setActiveJobID(job.job_id); setStopPending(false);
      await waitForJob(job.job_id, 'Writing your draft');
      const doc = await api(`/api/v1/articles/art_${trip.id}`);
      setArticle(doc); setScreen('editor');
    } catch (e) {
      if (e.message === 'Generation cancelled') setNotice('Generation stopped. You can start it again.');
      else setError(e.message);
    }
    finally { setBusy(false); setActiveJobID(''); setStopPending(false); }
  };

  const stopGeneration = async () => {
    if (!activeJobID || stopPending) return;
    setStopPending(true); setNotice('Stopping generation…');
    try { await api(`/api/v1/jobs/${activeJobID}/cancel`, { method: 'POST' }); }
    catch (e) { setStopPending(false); setError(`Could not stop generation: ${e.message}`); }
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
      const finalDoc = { ...article, lexical: JSON.stringify(initialLexical(article, photos, videos)) };
      finalDoc.blocks = blocksFromLexical(JSON.parse(finalDoc.lexical), article.blocks);
      setArticle(finalDoc);
      await api(`/api/v1/articles/${article.article_id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(finalDoc) });
      await api(`/api/v1/trips/${trip.id}/ghost-draft`, { method: 'POST' });
      setNotice('Ghost draft created');
    } catch (e) { setError(e.message); setNotice(''); }
    finally { setBusy(false); }
  };

  const saveModelSettings = async (event) => {
    event.preventDefault(); setError(''); setNotice('Saving model settings…');
    try {
      const settings = await api('/api/v1/settings/model', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider: 'openrouter', model: openRouterModel, api_key: openRouterKey }),
      });
      setModelProvider(settings.model_provider); setOpenRouterConfigured(settings.openrouter_configured);
      setOpenRouterModel(settings.openrouter_model); setOpenRouterKey(''); setNotice('OpenRouter is ready for text generation.');
    } catch (e) { setError(e.message); setNotice(''); }
  };

  const saveLocalSettings = async (event) => {
    event.preventDefault(); setError(''); setNotice('Saving local model settings…');
    try {
      const settings = await api('/api/v1/settings/model', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider: 'local', endpoint: localEndpoint, model: localModel, api_key: localKey }),
      });
      setModelProvider(settings.model_provider); setLocalConfigured(settings.local_configured);
      setLocalEndpoint(settings.local_endpoint); setLocalModel(settings.local_model); setLocalKey('');
      setNotice('Local OpenAI-compatible model is ready for text generation.');
    } catch (e) { setError(e.message); setNotice(''); }
  };

  const saveVisionSettings = async (event) => {
    event.preventDefault(); setError(''); setNotice('Saving vision model settings…');
    try {
      const settings = await api('/api/v1/settings/vision', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ endpoint: visionEndpoint, model: visionModel, api_key: visionKey }),
      });
      setVisionEndpoint(settings.vision_endpoint); setVisionModel(settings.vision_model);
      setVisionConfigured(settings.vision_configured); setVisionKey('');
      setNotice('Vision model settings saved.');
    } catch (e) { setError(e.message); setNotice(''); }
  };

  const activateModel = async (provider) => {
    setError('');
    try {
      const settings = await api('/api/v1/settings/model/activate', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider }),
      });
      setModelProvider(settings.model_provider);
      setNotice(`Using ${provider === 'local' ? 'the local model' : 'OpenRouter'} for text generation.`);
    } catch (e) { setError(e.message); }
  };

  const resetModelSettings = async () => {
    setError('');
    try {
      const settings = await api('/api/v1/settings/model', { method: 'DELETE' });
      setModelProvider(settings.model_provider); setLocalConfigured(settings.local_configured); setOpenRouterConfigured(settings.openrouter_configured);
      setLocalEndpoint(settings.local_endpoint || ''); setLocalModel(settings.local_model || ''); setOpenRouterModel(settings.openrouter_model || '');
      setOpenRouterKey(''); setLocalKey(''); setNotice('Switched away from OpenRouter.');
    } catch (e) { setError(e.message); }
  };

  const saveTimeoutSettings = async (event) => {
    event.preventDefault(); setError(''); setNotice('Saving model timeout…');
    try {
      const settings = await api('/api/v1/settings/timeout', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ seconds: Number(modelTimeoutSeconds) }),
      });
      setModelTimeoutSeconds(String(settings.model_timeout_sec)); setTimeoutSource(settings.timeout_source);
      setNotice('Model timeout saved. New model requests will use it.');
    } catch (e) { setError(e.message); setNotice(''); }
  };

  const resetTimeoutSettings = async () => {
    setError('');
    try {
      const settings = await api('/api/v1/settings/timeout', { method: 'DELETE' });
      setModelTimeoutSeconds(String(settings.model_timeout_sec)); setTimeoutSource(settings.timeout_source);
      setNotice('Model timeout now follows OMLX_TIMEOUT_SEC in .env.');
    } catch (e) { setError(e.message); }
  };

  const previewFiles = useMemo(() => selectedFiles.map((file) => ({ file, url: URL.createObjectURL(file) })), [selectedFiles]);
  const tripPreviewFiles=useMemo(()=>tripMediaFiles.map((file)=>({file,url:URL.createObjectURL(file)})),[tripMediaFiles]);
  const recommendedPhotos = photos.filter((photo) => photo.status === 'recommended');
  const visiblePhotos = photoFilter === 'recommended' ? recommendedPhotos : photos;
  useEffect(() => () => previewFiles.forEach((item) => URL.revokeObjectURL(item.url)), [previewFiles]);
  useEffect(() => () => tripPreviewFiles.forEach((item)=>URL.revokeObjectURL(item.url)),[tripPreviewFiles]);

  if (!authInfo) return <div className="app-shell"><main className="main-content"><p>Connecting to Yatra…</p></main></div>;
  if (!authInfo.authenticated) return <div className="app-shell"><main className="main-content"><section className="welcome"><p className="eyebrow">YATRA</p><h1>{authInfo.configured ? 'Welcome back' : 'Set up Yatra'}</h1><p className="intro">{authInfo.configured ? 'Enter your app password to continue.' : 'Create an app password to protect your photos, drafts, and settings.'}</p></section><section className="create-card"><form onSubmit={submitAuth}><label className="field">App password<input autoFocus type="password" autoComplete={authInfo.configured ? 'current-password' : 'new-password'} minLength={12} required value={authPassword} onChange={(e) => setAuthPassword(e.target.value)} /></label>{authError && <p className="message message-error" role="alert">{authError}</p>}<button className="button button-primary full-button" disabled={authBusy}>{authBusy ? 'Unlocking…' : authInfo.configured ? 'Unlock Yatra' : 'Create password'}</button></form></section></main></div>;
  return <div className="app-shell">
    <header className="topbar">
      <button className="brand" onClick={() => { setScreen('home'); setTrip(null); setArticle(null); loadTrips(); }} aria-label="Yatra home"><span className="brand-mark">Y</span><span>Yatra</span></button>
      <span className="topbar-note">Turn trip notes into memorable blog posts</span>
      <div className="topbar-actions"><button className="text-button" onClick={async () => { setError(''); try { await loadSettings(); setScreen('app-settings'); } catch (e) { setError(e.message); } }}>Settings</button><button className="text-button" onClick={() => { setError(''); setNotice(''); setScreen('model-settings'); }}>Model settings</button><button className="text-button" onClick={async () => { await api('/api/v1/auth/logout', { method: 'POST' }); setAuthInfo({ ...(authInfo || {}), authenticated: false }); setScreen('home'); }}>Lock</button></div>
    </header>
    <main className="main-content">
      {(error || notice) && <div className={`message ${error ? 'message-error' : ''}`} role={error ? 'alert' : 'status'}><span>{error || notice}</span>{activeJobID && !error && <button className="stop-generation" onClick={stopGeneration} disabled={stopPending} aria-label="Stop generation">{stopPending ? 'Stopping…' : 'Stop'}</button>}</div>}

      {screen === 'app-settings' && <>
        <button className="back-link" onClick={() => setScreen('home')}>← Back to stories</button>
        <section className="welcome"><p className="eyebrow">YATRA</p><h1>Settings</h1><p className="intro">Credentials are encrypted on this server and never shown again after saving. Leave a key blank to keep the saved key.</p></section>
        {settings && <form className="create-card settings-form" onSubmit={saveSettings}>
          <p className="eyebrow settings-section">GHOST</p>
          <label className="field">Ghost site URL<input value={settings.ghost_url} onChange={(e) => setSettings({ ...settings, ghost_url: e.target.value })} placeholder="https://yourblog.example" /><span className="field-help">Use the site URL without /ghost/api/admin.</span></label>
          <label className="field">Ghost Admin API key <span className="field-help">{settings.ghost_key_configured ? 'A key is saved. Leave blank to keep it.' : 'No key saved.'}</span><input type="password" autoComplete="new-password" value={settings.ghost_admin_key || ''} onChange={(e) => setSettings({ ...settings, ghost_admin_key: e.target.value })} /></label>
          <p className="eyebrow settings-section">PLACE RESEARCH</p>
          <label className="check-field"><input type="checkbox" checked={settings.web_research} onChange={(e) => setSettings({ ...settings, web_research: e.target.checked })} /> Search the web for destination facts</label>
          <p className="field-help">Off by default. When off, your configured model provides place background. When on, Yatra uses Brave Search.</p>
          <label className="field">Brave Search API key <span className="field-help">{settings.brave_key_configured ? 'A key is saved. Leave blank to keep it.' : 'No key saved.'}</span><input type="password" autoComplete="new-password" value={settings.brave_key || ''} onChange={(e) => setSettings({ ...settings, brave_key: e.target.value })} /></label>
          <p className="eyebrow settings-section">PHOTOS AND UPLOADS</p>
          <div className="field-row settings-grid"><label className="field">Thumbnail size (px)<input type="number" min="100" value={settings.thumbnail_size} onChange={(e) => setSettings({ ...settings, thumbnail_size: Number(e.target.value) })} /></label><label className="field">Preview size (px)<input type="number" min="100" value={settings.preview_size} onChange={(e) => setSettings({ ...settings, preview_size: Number(e.target.value) })} /></label><label className="field">Ghost image size (px)<input type="number" min="100" value={settings.export_size} onChange={(e) => setSettings({ ...settings, export_size: Number(e.target.value) })} /></label><label className="field">JPEG quality (30–100)<input type="number" min="30" max="100" value={settings.jpeg_quality} onChange={(e) => setSettings({ ...settings, jpeg_quality: Number(e.target.value) })} /></label></div>
          <p className="field-help">Image settings apply to new uploads; existing photo derivatives are not regenerated.</p>
          <div className="field-row settings-grid"><label className="field">Maximum size per photo (MB)<input type="number" min="1" value={settings.max_file_size_mb} onChange={(e) => setSettings({ ...settings, max_file_size_mb: Number(e.target.value) })} /></label><label className="field">Maximum upload total (MB)<input type="number" min="1" value={settings.max_upload_size_mb} onChange={(e) => setSettings({ ...settings, max_upload_size_mb: Number(e.target.value) })} /></label><label className="field">Photos per upload<input type="number" min="1" max="1000" value={settings.max_upload_files} onChange={(e) => setSettings({ ...settings, max_upload_files: Number(e.target.value) })} /></label></div>
          <button className="button button-primary full-button" disabled={settingsBusy}>{settingsBusy ? 'Saving…' : 'Save settings'}</button>
        </form>}
      </>}

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
            <div className="field-row"><label className="field">Video export profile<select value={videoProfile} onChange={(e)=>setVideoProfile(e.target.value)}><option value="standard">Standard · up to 1920 × 1080</option><option value="lightweight">Lightweight · up to 1280 × 720</option></select><span className="field-help">Default selection is six seconds. There is no duration cap.</span></label></div>
            <div className="upload-head"><div><label className="field-label">Choose photos and videos</label><p className="field-help">JPG, PNG, HEIC, MP4, MOV or WebM. Trim every video to the section you want; the default is the first {videoDefaultDuration} seconds.</p></div><span className="photo-count">{selectedFiles.length} of {maxUploadFiles} selected</span></div>
            <input ref={fileRef} className="file-input" type="file" multiple accept="image/*,video/*,.heic,.heif" onChange={addSelectedFiles} />
            <button type="button" className="pick-button" disabled={selectedFiles.length >= maxUploadFiles} onClick={() => fileRef.current?.click()}><span className="plus">＋</span><span>{selectedFiles.length ? 'Add more media' : 'Choose media from this device'}</span></button>
            {previewFiles.length > 0 && <div className="preview-grid">{previewFiles.map(({ file, url }, i) => <div className="local-photo" key={`${file.name}-${i}`}>{isVideoFile(file) ? <><video src={url} controls muted playsInline {...trimPreviewProps(videoTrims[file.name]?.start??0,videoTrims[file.name]?.end??videoDefaultDuration,(duration)=>recordVideoDuration(file,duration))} /><strong>{file.name}</strong>{videoDurations[file.name] > 0 && <div className="video-trim"><label>Start (seconds)<input aria-label={`Start time in seconds for ${file.name}`} type="number" min="0" max={Math.max(0,videoDurations[file.name]-0.1)} step="0.1" value={videoTrims[file.name]?.start ?? 0} onChange={(e)=>setVideoTrims((t)=>({...t,[file.name]:{...t[file.name],start:Number(e.target.value)}}))}/></label><label>End (seconds)<input aria-label={`End time in seconds for ${file.name}`} type="number" min="0.1" max={videoDurations[file.name]} step="0.1" value={videoTrims[file.name]?.end ?? Math.min(videoDefaultDuration,videoDurations[file.name])} onChange={(e)=>setVideoTrims((t)=>({...t,[file.name]:{...t[file.name],end:Number(e.target.value)}}))}/></label><span>{Math.max(0,(videoTrims[file.name]?.end ?? Math.min(videoDefaultDuration,videoDurations[file.name]))-(videoTrims[file.name]?.start ?? 0)).toFixed(1)} sec</span></div>}</> : <img src={url} alt={file.name} />}<button type="button" className="remove-local-photo" title={`Remove ${file.name}`} onClick={()=>setSelectedFiles((items)=>items.filter((_,index)=>index!==i))}>×</button></div>)}</div>}
            <button className="button button-primary full-button" disabled={busy || selectedFiles.length === 0}>{busy ? 'Preparing…' : `Upload ${selectedFiles.length || ''} ${selectedFiles.length === 1 ? 'file' : 'files'} and review`}</button>
          </form>
        </section>
        {trips?.length > 0 && <section className="recent-section"><div className="section-title"><div><p className="eyebrow">PICK UP WHERE YOU LEFT OFF</p><h2>Your stories</h2></div></div><div className="trip-list">{trips.map((item) => <div key={item.id} className="trip-row"><button className="trip-open-button" onClick={() => openTrip(item.id)}><span><strong>{item.name}</strong><small>{item.location || 'Travel story'} · {new Date(item.updated_at).toLocaleDateString()}</small></span><span className="trip-status">{item.status.toLowerCase().replaceAll('_', ' ')}</span><span className="arrow">→</span></button><button className="trip-delete-button" title={`Delete ${item.name}`} aria-label={`Delete ${item.name}`} onClick={(event) => deleteTrip(event, item)}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3.5 5.5h13M8 5.5V3.8h4v1.7m-6.5 0 .8 10.7h7.4l.8-10.7M8.2 8.2v5.5m3.6-5.5v5.5" /></svg></button></div>)}</div></section>}
      </>}

      {screen === 'model-settings' && <>
        <button className="back-link" onClick={() => setScreen('home')}>← All stories</button>
        <section className="welcome settings-welcome"><p className="eyebrow">MODEL SETTINGS</p><h1>Choose your writing model.</h1><p className="intro">Configure a local OpenAI-compatible endpoint or OpenRouter separately. Model requests include your trip notes and photo descriptions.</p></section>
        <section className="create-card settings-card">
          <div className="section-title"><div><p className="eyebrow">LOCAL · OPENAI-COMPATIBLE TEXT MODEL</p><h2>{localConfigured ? 'Local model configured' : 'Configure local model'}</h2></div><span className={`model-status ${localConfigured ? 'configured' : ''}`}>{modelProvider === 'local' ? 'Active' : localConfigured ? 'Ready' : 'Not configured'}</span></div>
          <form onSubmit={saveLocalSettings}>
            <label className="field">Base URL<input required type="url" value={localEndpoint} onChange={(e) => setLocalEndpoint(e.target.value)} placeholder="http://host.docker.internal:8000/v1" autoComplete="url" /></label>
            <label className="field">Local model ID<input required value={localModel} onChange={(e) => setLocalModel(e.target.value)} placeholder="your-local-model" autoComplete="off" /></label>
            <label className="field">Local API key, if required<input type="password" value={localKey} onChange={(e) => setLocalKey(e.target.value)} placeholder="Leave blank to use OMLX_API_KEY from .env, if set" autoComplete="new-password" /></label>
            <p className="field-help settings-help">The base URL and model ID are saved locally. The key is held in memory only, is never saved to the database, browser, or logs, and must be re-entered after restart if required. It is sent in the authorization header to this endpoint; use a trusted local endpoint, or HTTPS for a remote one.</p>
            <button className="button button-primary full-button" disabled={!localEndpoint.trim() || !localModel.trim()}>Save and use local model</button>
          </form>
          {localConfigured && modelProvider !== 'local' && <button className="text-button reset-model" onClick={() => activateModel('local')}>Use local model</button>}
        </section>
        <section className="create-card settings-card provider-card">
          <div className="section-title"><div><p className="eyebrow">PHOTO ANALYSIS · OPENAI-COMPATIBLE</p><h2>{visionConfigured ? 'Vision model configured' : 'Configure vision model'}</h2></div><span className={`model-status ${visionConfigured ? 'configured' : ''}`}>{visionConfigured ? 'Ready' : 'Not configured'}</span></div>
          <form onSubmit={saveVisionSettings}>
            <label className="field">Vision endpoint Base URL<input required type="url" value={visionEndpoint} onChange={(e) => setVisionEndpoint(e.target.value)} placeholder="http://host.docker.internal:8000/v1" autoComplete="url" /></label>
            <label className="field">Vision model ID<input required value={visionModel} onChange={(e) => setVisionModel(e.target.value)} placeholder="your-vision-model" autoComplete="off" /></label>
            <label className="field">Vision API key, if required<input type="password" value={visionKey} onChange={(e) => setVisionKey(e.target.value)} placeholder="Leave blank to use OMLX_API_KEY from .env, if set" autoComplete="new-password" /></label>
            <p className="field-help settings-help">Endpoint and model are saved locally. The key is memory-only, is never saved to the database, browser, or logs, and must be re-entered after restart if required. Selected photos are sent to this endpoint for analysis; use a trusted local endpoint, or HTTPS for a remote one.</p>
            <button className="button button-primary full-button" disabled={!visionEndpoint.trim() || !visionModel.trim()}>Save vision settings</button>
          </form>
        </section>
        <section className="create-card settings-card provider-card">
          <div className="section-title"><div><p className="eyebrow">OPENROUTER</p><h2>{openRouterConfigured ? 'OpenRouter configured' : 'Configure OpenRouter'}</h2></div><span className={`model-status ${openRouterConfigured ? 'configured' : ''}`}>{modelProvider === 'openrouter' ? 'Active' : openRouterConfigured ? 'Ready' : 'Not configured'}</span></div>
          <form onSubmit={saveModelSettings}>
            <label className="field">OpenRouter model ID<input required value={openRouterModel} onChange={(e) => setOpenRouterModel(e.target.value)} placeholder="openai/gpt-4o-mini" autoComplete="off" /></label>
            <label className="field">OpenRouter API key<input required type="password" value={openRouterKey} onChange={(e) => setOpenRouterKey(e.target.value)} placeholder="sk-or-v1-…" autoComplete="new-password" /></label>
            <p className="field-help settings-help">The key is held in server memory only and is cleared when Yatra restarts. It is never sent back to this page, saved in the browser, database, or logs. OpenRouter receives it over HTTPS with each model request.</p>
            <button className="button button-primary full-button" disabled={!openRouterModel.trim() || !openRouterKey}>Save OpenRouter settings</button>
          </form>
          {openRouterConfigured && modelProvider !== 'openrouter' && <button className="text-button reset-model" onClick={() => activateModel('openrouter')}>Use OpenRouter</button>}
          {modelProvider === 'openrouter' && <button className="text-button reset-model" onClick={resetModelSettings}>Return to local or startup model</button>}
        </section>
        <section className="create-card settings-card timeout-card">
          <div className="section-title"><div><p className="eyebrow">ALL MODEL PROVIDERS</p><h2>Request timeout</h2></div><span className="model-status configured">{timeoutSource === 'settings' ? 'Saved in settings' : timeoutSource === 'env' ? 'From .env' : 'Not set'}</span></div>
          <form onSubmit={saveTimeoutSettings}>
            <label className="field">Timeout in seconds<input required type="number" min="0" max="9223372036" step="1" value={modelTimeoutSeconds} onChange={(e) => setModelTimeoutSeconds(e.target.value)} placeholder="Set OMLX_TIMEOUT_SEC in .env" /></label>
            <p className="field-help settings-help">This applies to every text and vision model request. A saved UI value overrides OMLX_TIMEOUT_SEC and survives restarts. Reset to use the .env value. Set to 0 for no client-side timeout.</p>
            <button className="button button-primary full-button" disabled={modelTimeoutSeconds === '' || !Number.isSafeInteger(Number(modelTimeoutSeconds)) || Number(modelTimeoutSeconds) < 0 || Number(modelTimeoutSeconds) > 9223372036}>Save timeout</button>
          </form>
          {timeoutSource === 'settings' && <button className="text-button reset-model" onClick={resetTimeoutSettings}>Use OMLX_TIMEOUT_SEC from .env</button>}
        </section>
      </>}

      {screen === 'trip' && trip && <>
        <button className="back-link" onClick={() => setScreen('home')}>← All stories</button>
        <section className="trip-heading"><p className="eyebrow">{trip.location || 'YOUR TRIP'}</p><h1>{trip.name}</h1><p className="intro">Choose the photos and clips that tell the story. We’ll use your notes and destination to write a first draft.</p></section>
        {trip.notes && <section className="notes-card"><p className="eyebrow">YOUR NOTES</p><p>{trip.notes}</p></section>}
        {article ? <button className="button button-primary" onClick={openEditor}>Continue editing this draft</button> : <>
          <div className="review-toolbar"><div><h2>Media review</h2><p>{photos.filter((p) => p.status !== 'not_selected').length + videos.filter((v)=>v.selected).length} selected</p><p className="field-help">Choose photos and videos for your story. Tap the star on a photo to use it as the post header image.</p></div><div className="review-tools"><button className="text-button" onClick={async () => { const next = photos.map((p) => ({ ...p, status: 'recommended' })); setPhotos(next); try { await api(`/api/v1/trips/${trip.id}/photos/select-all`, { method: 'POST' }); } catch (e) { setError(e.message); try { setPhotos(await api(`/api/v1/trips/${trip.id}/photos`)); } catch { /* retain optimistic state if refresh fails */ } } }}>Select all photos</button><input ref={tripMediaRef} className="file-input" type="file" multiple accept="image/*,video/*,.heic,.heif" onChange={addTripMedia}/><button type="button" className="text-button" onClick={()=>tripMediaRef.current?.click()}>Add photos or videos</button></div></div>
          {tripPreviewFiles.length>0&&<section className="create-card"><h3>New media</h3><div className="preview-grid">{tripPreviewFiles.map(({file,url},i)=><div className="local-photo" key={`${file.name}-${i}`}>{isVideoFile(file)?<><video src={url} controls muted playsInline {...trimPreviewProps(tripMediaTrims[file.name]?.start??0,tripMediaTrims[file.name]?.end??videoDefaultDuration,(duration)=>{setTripMediaDurations(d=>({...d,[file.name]:duration}));setTripMediaTrims(t=>t[file.name]?t:{...t,[file.name]:{start:0,end:Math.min(videoDefaultDuration,duration)}});})}/><strong>{file.name}</strong>{tripMediaDurations[file.name]>0&&<div className="video-trim"><label>Start (sec)<input type="number" min="0" step="0.1" value={tripMediaTrims[file.name]?.start??0} onChange={(e)=>setTripMediaTrims(t=>({...t,[file.name]:{...t[file.name],start:Number(e.target.value)}}))}/></label><label>End (sec)<input type="number" min="0.1" max={tripMediaDurations[file.name]} step="0.1" value={tripMediaTrims[file.name]?.end??Math.min(videoDefaultDuration,tripMediaDurations[file.name])} onChange={(e)=>setTripMediaTrims(t=>({...t,[file.name]:{...t[file.name],end:Number(e.target.value)}}))}/></label></div>}</>:<img src={url} alt={file.name}/>}<button type="button" className="remove-local-photo" onClick={()=>setTripMediaFiles(list=>list.filter((_,index)=>index!==i))}>×</button></div>)}</div><label className="field">Video export profile<select value={videoProfile} onChange={e=>setVideoProfile(e.target.value)}><option value="standard">Standard</option><option value="lightweight">Lightweight</option></select></label><button type="button" className="button button-primary" disabled={busy} onClick={uploadTripMedia}>{busy?'Processing…':'Upload to this trip'}</button></section>}
          <div className="photo-filters" role="group" aria-label="Filter photos">
            <button type="button" className={photoFilter === 'all' ? 'active' : ''} onClick={() => setPhotoFilter('all')}>All photos ({photos.length})</button>
            <button type="button" className={photoFilter === 'recommended' ? 'active' : ''} onClick={() => setPhotoFilter('recommended')}>Recommended ({recommendedPhotos.length})</button>
          </div>
          <div className="photo-grid">{visiblePhotos.map((photo) => { const chosen = photo.status !== 'not_selected'; const isFeature = featurePhotoID === photo.id; return <div key={photo.id} className="photo-tile"><button className={`photo-card ${chosen ? 'chosen' : ''}`} onClick={() => setPhotoSelected(photo, !chosen)} aria-pressed={chosen} title={photo.description || undefined}><span className="photo-image"><img src={photo.thumbnail} alt={photo.description || 'Trip photo'} loading="lazy" /><span className="photo-check">{chosen ? '✓' : '+'}</span></span>{photo.description && <span className="photo-description">{photo.description}</span>}</button><button className={`photo-feature-button ${isFeature ? 'active' : ''}`} onClick={() => chooseFeaturePhoto(photo)} aria-label={isFeature ? 'Header image selected' : 'Use as post header image'} aria-pressed={isFeature} title={isFeature ? 'Header image' : 'Use as post header image'}>{isFeature ? '★' : '☆'}</button></div>; })}</div>
          {videos.length > 0 && <><h3 className="video-review-title">Video clips</h3><div className="video-review-grid">{videos.map((video)=>{const edit=videoEditSettings[video.id]||{};return <div key={video.id} className={`video-review-card ${video.selected?'chosen':''}`}><video src={video.url} poster={video.poster_url} controls muted playsInline preload="none" /><span>{video.file_name} · {video.duration.toFixed(1)} sec · {video.profile} · {(video.source_bytes/1048576).toFixed(1)} → {(video.output_bytes/1048576).toFixed(1)} MB ({video.output_bytes&&video.source_bytes?Math.max(0,Math.round((1-video.output_bytes/video.source_bytes)*100)):0}% smaller)</span><small>Original: {video.source_width}×{video.source_height} · {video.source_codec} · {video.source_has_audio?'has audio (removed)':'silent'}</small>{video.status==='failed'&&<small role="alert">Processing failed: {video.error}</small>}{video.status==='processing'&&<small>Processing…</small>}<div className="video-trim"><label>Start (sec)<input type="number" min="0" max={Math.max(0,(video.source_duration||video.duration)-0.1)} step="0.1" value={edit.start??video.start_time??0} onChange={(e)=>setVideoEditSettings((all)=>({...all,[video.id]:{...edit,start:Number(e.target.value)}}))}/></label><label>End (sec)<input type="number" min="0.1" max={video.source_duration||video.duration} step="0.1" value={edit.end??video.end_time??video.duration} onChange={(e)=>setVideoEditSettings((all)=>({...all,[video.id]:{...edit,end:Number(e.target.value)}}))}/></label></div><label className="field">Export profile<select value={edit.profile||video.profile||'standard'} onChange={(e)=>setVideoEditSettings((all)=>({...all,[video.id]:{...edit,profile:e.target.value}}))}><option value="standard">Standard</option><option value="lightweight">Lightweight</option></select></label><span>Selected duration: {Math.max(0,(edit.end??video.end_time??video.duration)-(edit.start??video.start_time??0)).toFixed(1)} sec</span><button type="button" disabled={videoProcessingID===video.id||video.status==='processing'} onClick={()=>reprocessVideo(video)}>{videoProcessingID===video.id?'Processing…':'Preview / reprocess'}</button><button type="button" onClick={()=>setVideoSelected(video,!video.selected)} aria-pressed={video.selected}>{video.selected?'Remove from draft':'Include in draft'}</button></div>})}</div></>}
          <button className="button button-primary full-button draft-button" disabled={busy || (photos.length === 0 && videos.length === 0)} onClick={generate}>{busy ? 'Writing your draft…' : 'Create my draft'}<span>→</span></button>
        </>}
      </>}

      {screen === 'editor' && article && <>
        <button className="back-link" onClick={() => setScreen('trip')}>← Photo review</button>
        <div className="editor-head"><div><p className="eyebrow">DRAFT · {trip?.location}</p><h1>Make it yours</h1><p className="intro">Edit the story below. Your changes save automatically.</p></div><div className="editor-actions"><span className="save-status">{notice}</span><button className="button button-ghost" onClick={pushToGhost} disabled={busy}>Send draft to Ghost</button></div></div>
        {article.research_summary && <details className="research-card"><summary>Place background used in this draft</summary><p>{article.research_summary}</p>{article.research_sources?.map((source) => <a key={source} href={source} target="_blank" rel="noreferrer">{new URL(source).hostname}</a>)}</details>}
        <article className="article-editor"><label className="field title-field">Post title<input value={article.title} onChange={(e) => { const next = { ...article, title: e.target.value, slug: e.target.value.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') }; setArticle(next); clearTimeout(saveTimer.current); saveTimer.current = setTimeout(() => api(`/api/v1/articles/${article.article_id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(next) }).then(() => setNotice('Saved')).catch((err) => setError(err.message)), 850); }} /></label>
          <React.Suspense fallback={<div className="editor-loading">Opening the editor…</div>}>
            <WritingEditor key={article.article_id} initialEditorState={initialLexical(article, photos, videos)} fileUploader={editorFileUploader} onChange={saveArticle} />
          </React.Suspense>
        </article>
      </>}
    </main>
    <footer className="footer">Made for the stories behind the photos.</footer>
  </div>;
}

createRoot(document.getElementById('root')).render(<React.StrictMode><App /></React.StrictMode>);

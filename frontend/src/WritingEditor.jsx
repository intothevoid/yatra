import React from 'react';
import { KoenigComposer, KoenigEditor } from '@tryghost/koenig-lexical';
import '@tryghost/koenig-lexical/style.css';

export default function WritingEditor({ initialEditorState, fileUploader, onChange }) {
  return <KoenigComposer initialEditorState={initialEditorState} fileUploader={fileUploader} cardConfig={{ post: { visibility: 'public' } }}>
    <KoenigEditor onChange={onChange} placeholderText="Start writing your story…" />
  </KoenigComposer>;
}

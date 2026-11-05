import { useState } from 'react';
import { Archive, FolderOpen, LoaderCircle } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { OscChatbox } from '../components/osc-chatbox';
import { OscParameters } from '../components/osc-parameters';
import { SectionPage } from '../components/section-page';
import { UsernameChecker } from '../components/username-checker';

export default function DesktopTools() {
  useLocale();
  const [metadata, setMetadata] = useState(true);
  const [extracting, setExtracting] = useState(false);
  const [extraction, setExtraction] = useState<{ folder: string; files: number } | null>(null);
  const [extractError, setExtractError] = useState('');
  async function extract() {
    setExtracting(true);
    setExtractError('');
    setExtraction(null);
    try {
      const result = await api('/desktop/tools/extract', { metadata });
      if (!result.cancelled) setExtraction(result);
    } catch (e) {
      setExtractError((e as Error).message);
    } finally {
      setExtracting(false);
    }
  }
  return (
    <SectionPage title="Tools" subtitle="Small tools for VRChat, running on this computer.">
      <div className="desktop-tools-grid">
        <UsernameChecker />
        <OscChatbox />
        <div className="desktop-tools-column">
          <OscParameters />
          <section className="dashboard-panel desktop-tool login-content" aria-labelledby="extract-title">
            <div className="desktop-tool-heading">
              <Archive size={20} />
              <div>
                <h3 id="extract-title">{tr('Unity package extractor')}</h3>
                <p>{tr('Get the original files without opening Unity.')}</p>
              </div>
            </div>
            <p className="fineprint">
              {tr(
                'Choose a .unitypackage and a destination folder. VRCove creates a new folder there and restores the asset names and folders.',
              )}
            </p>
            <label className="tool-checkbox">
              <input
                type="checkbox"
                checked={metadata}
                disabled={extracting}
                onChange={(e) => setMetadata(e.target.checked)}
              />
              {tr('Include Unity .meta files')}
            </label>
            <button className="standard-button" disabled={extracting} onClick={() => void extract()}>
              {extracting ? <LoaderCircle size={14} className="spin" /> : <FolderOpen size={14} />}
              {tr(extracting ? 'Choosing or extracting...' : 'Choose package and extract')}
            </button>
            {extractError && (
              <p className="form-error" role="alert">
                {extractError}
              </p>
            )}
            {extraction && (
              <div className="extraction-result" role="status">
                <strong>{tr('Extracted {count} files', { count: extraction.files })}</strong>
                <code>{extraction.folder}</code>
              </div>
            )}
            <p className="fineprint">
              {tr('Your files stay on this computer. Package scripts are never run.')}
            </p>
          </section>
        </div>
      </div>
    </SectionPage>
  );
}

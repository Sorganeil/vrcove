import { useEffect, useState } from 'react';
import { ShieldCheck, Download, Upload, Trash2, ExternalLink } from 'lucide-react';
import DesktopNotificationSettings from '../components/desktop-notification-settings';
import LanguagePicker from '../components/language-picker';
import PrivacyApproach from '../components/privacy-approach';
import { t as tr, useLocale } from '../lib/i18n';
import type { Avatar } from '../lib/avatar';

/** Privacy & settings: language, desktop notifications, and the bookmarks and notes kept in this browser. */
export default function Settings({
  notify,
  saved,
  setSaved,
}: {
  notify: (message: string) => void;
  saved: Avatar[];
  setSaved: (avatars: Avatar[]) => void;
}) {
  useLocale();
  const [confirm, setConfirm] = useState<{ action: string; text: string } | null>(null),
    [notes, setNotes] = useState('');
  useEffect(() => {
    try {
      setNotes(localStorage.getItem('vrcove-notes') ?? localStorage.getItem('veilchat-notes') ?? '');
    } catch {
      /* browser storage unavailable */
    }
  }, []);
  function exportData() {
    const blob = new Blob([JSON.stringify({ version: 1, bookmarks: saved, notes }, null, 2)], {
        type: 'application/json',
      }),
      url = URL.createObjectURL(blob),
      a = document.createElement('a');
    a.href = url;
    a.download = 'vrcove-local-data.json';
    a.click();
    URL.revokeObjectURL(url);
  }
  async function importData(e: any) {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      if (file.size > 5000000) throw new Error('Choose a backup smaller than 5 MB.');
      const backup = JSON.parse(await file.text());
      if (backup.version !== 1 || !Array.isArray(backup.bookmarks) || backup.bookmarks.length > 500)
        throw new Error('This is not a supported VRCove backup.');
      const merged = [...saved];
      for (const a of backup.bookmarks) {
        if (
          !/^avtr_[a-f0-9-]{36}$/.test(a.id) ||
          typeof a.name !== 'string' ||
          typeof a.author !== 'string' ||
          !Array.isArray(a.tags)
        )
          throw new Error('Invalid bookmark in backup.');
        if (!merged.some((b) => b.id === a.id)) merged.push(a);
      }
      const next = merged.slice(0, 500);
      localStorage.setItem('atlas-bookmarks', JSON.stringify(next));
      setSaved(next);
      if (typeof backup.notes === 'string') {
        localStorage.setItem('vrcove-notes', backup.notes.slice(0, 100000));
        localStorage.removeItem('veilchat-notes');
        setNotes(backup.notes.slice(0, 100000));
      }
      notify(tr('Local data imported.'));
    } catch (e) {
      notify((e as Error).message);
    }
    e.target.value = '';
  }
  return (
    <div className="settings-page">
      <div className="page-title">
        <div>
          <h1>{tr('Privacy & settings')}</h1>
          <p>{tr('Control what VRCove keeps on this device.')}</p>
        </div>
        <ShieldCheck size={25} />
      </div>
      <section className="dashboard-panel language-settings">
        <LanguagePicker />
      </section>
      <DesktopNotificationSettings notify={notify} />
      <PrivacyApproach />
      <div className="settings-grid">
        <section className="dashboard-panel">
          <div className="panel-title">
            <h2>{tr('Local data')}</h2>
          </div>
          <div className="settings-content">
            <p>
              {saved.length}{' '}
              {tr('bookmarks saved in this browser. Export a backup before clearing browser data.')}
            </p>
            <div className="settings-actions">
              <button className="standard-button" onClick={exportData}>
                <Download size={14} />
                {tr('Export backup')}
              </button>
              <label className="standard-button file-label">
                <Upload size={14} />
                {tr('Import backup')}
                <input
                  aria-label={tr('Import local backup')}
                  type="file"
                  accept="application/json"
                  onChange={importData}
                />
              </label>
              <button
                className="standard-button danger-text"
                onClick={() =>
                  setConfirm({
                    action: 'clearLocal',
                    text: 'Clear all local bookmarks and notes? This cannot be undone without a backup.',
                  })
                }
              >
                <Trash2 size={14} />
                {tr('Clear local data')}
              </button>
            </div>
          </div>
        </section>
        <section className="dashboard-panel">
          <div className="panel-title">
            <h2>{tr('Browser-only limits')}</h2>
          </div>
          <div className="settings-content">
            <p>
              {tr(
                'VRCove cannot read VRChat game logs, track time while closed, run overlays, control Discord, or manage game files. The optional desktop companion doesn’t add any of this either: it only finds avatar IDs for the shared catalog.',
              )}
            </p>
            <p>
              {tr(
                'Payments, identity verification, account recovery, marketplace purchases, and advanced group administration use VRChat’s official website.',
              )}
            </p>
            <a className="text-link" href="https://vrchat.com/home" target="_blank" rel="noreferrer">
              {tr('Open VRChat account settings')}
              <ExternalLink size={13} />
            </a>
          </div>
        </section>
      </div>
      {confirm && (
        <div className="modal-backdrop">
          <div
            className="modal confirm-content"
            role="dialog"
            aria-modal="true"
            aria-label={tr('Clear local data')}
          >
            <h2>{tr('Clear local data?')}</h2>
            <p>{confirm.text}</p>
            <button
              className="primary"
              onClick={() => {
                localStorage.removeItem('atlas-bookmarks');
                localStorage.removeItem('vrcove-notes');
                localStorage.removeItem('veilchat-notes');
                setSaved([]);
                setNotes('');
                setConfirm(null);
                notify(tr('Local data cleared.'));
              }}
            >
              {tr('Clear bookmarks and notes')}
            </button>
            <button onClick={() => setConfirm(null)}>{tr('Cancel')}</button>
          </div>
        </div>
      )}
    </div>
  );
}

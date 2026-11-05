import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Eraser, LoaderCircle, MessageSquare, Play, Plus, Send, Square, X } from 'lucide-react';
import { api } from '../lib/api';
import { renderChatbox, chatboxLimit } from '../lib/chatbox-text';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';

type Settings = {
  template: string;
  status: string;
  port: number;
  interval: number;
  sound: boolean;
  rotation: string[];
  afkText: string;
  phrases: string[];
};
type ChatboxState = {
  settings: Settings;
  automatic: boolean;
  error: string;
  values?: Record<string, string>;
};
const defaults: Settings = {
  template: '',
  status: '',
  port: 9000,
  interval: 10,
  sound: false,
  rotation: [],
  afkText: '',
  phrases: [],
};
const sensors = ['media', 'cpu', 'gpu', 'ram'];
const insertable: Array<[token: string, label: string]> = [
  ['{time}', 'Time'],
  ['{time12}', '12-hour time'],
  ['{date}', 'Date'],
  ['{rotate}', 'Rotating message'],
  ['{media}', 'Now playing'],
  ['{cpu}', 'CPU'],
  ['{gpu}', 'GPU'],
  ['{ram}', 'RAM'],
  ['{world}', 'World'],
  ['{players}', 'Players'],
  ['{playtime}', 'Play time'],
];
const usesLiveValues = /\{(media|cpu|gpu|ram|world|players|playtime)\}/;

function values(s: Settings, live: Record<string, string> | undefined, now: Date) {
  const hours = now.getHours();
  const minutes = String(now.getMinutes()).padStart(2, '0');
  return {
    ...Object.fromEntries(
      ['media', 'cpu', 'gpu', 'ram', 'world', 'players', 'playtime', 'afktime'].map((k) => [k, '']),
    ),
    ...live,
    time: `${String(hours).padStart(2, '0')}:${minutes}`,
    time12: `${hours % 12 || 12}:${minutes} ${hours < 12 ? 'AM' : 'PM'}`,
    date: [
      now.getFullYear(),
      String(now.getMonth() + 1).padStart(2, '0'),
      String(now.getDate()).padStart(2, '0'),
    ].join('-'),
    status: s.status,
    rotate: s.rotation.find((line) => line.trim()) || '',
  };
}

/* Says what the row of buttons under it is for. */
function RowHeading({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="tool-row-heading">
      <strong>{title}</strong>
      {hint && <small className="fineprint">{hint}</small>}
    </div>
  );
}

/* Everything a message can do in VRChat's chatbox: one-off text, a repeating
   template with live values, and typing straight into the game. */
export function OscChatbox() {
  useLocale();
  const { user } = useApp();
  const [settings, setSettings] = useState(defaults);
  const [remote, setRemote] = useState<ChatboxState | null>(null);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState('');
  const [now, setNow] = useState(new Date());
  const [quick, setQuick] = useState('');
  const [keyboard, setKeyboard] = useState(false);
  const [recent, setRecent] = useState<string[]>([]);
  const typing = useRef(false);
  const template = useRef<HTMLTextAreaElement>(null);
  const all = [settings.template, settings.afkText, ...settings.rotation].join('\n');
  const wanted = sensors.filter((name) => all.includes(`{${name}}`)).join(',');
  const path = '/desktop/tools/chatbox' + (wanted ? `?want=${wanted}` : '');
  const live = usesLiveValues.test(all);
  useEffect(() => {
    let alive = true;
    api('/desktop/tools/chatbox')
      .then((d: ChatboxState) => {
        if (alive) {
          setSettings({ ...defaults, ...d.settings });
          setRemote(d);
        }
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, []);
  useEffect(() => {
    if (!remote?.automatic && !live) return;
    let alive = true;
    const refresh = () =>
      api(path)
        .then((d: ChatboxState) => {
          if (alive) setRemote(d);
        })
        .catch((e) => {
          if (alive) setError(e.message);
        });
    void refresh();
    const timer = window.setInterval(refresh, 3000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [remote?.automatic, live, path]);
  const previewValues = values(settings, remote?.values, now);
  const text = renderChatbox(
    settings.template.replaceAll('{rotate}', previewValues.rotate || '{rotate}'),
    previewValues,
  );
  const valid = !!text.trim();
  const locked = !!remote?.automatic;
  async function act(action: string) {
    setBusy(action);
    setError('');
    setMessage('');
    try {
      const d: ChatboxState = await api('/desktop/tools/chatbox', { ...settings, action });
      setRemote(d);
      setMessage(
        tr(
          action === 'save'
            ? 'Chatbox settings saved'
            : action === 'stop'
              ? 'Automatic messages stopped'
              : action === 'clear'
                ? 'Chatbox cleared'
                : 'Sent to VRChat',
        ),
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  function setTyping(next: boolean) {
    if (typing.current === next) return;
    typing.current = next;
    api('/desktop/tools/chatbox', { action: 'typing', typing: next }).catch(() => {});
  }
  function insert(token: string) {
    const start = template.current?.selectionStart ?? settings.template.length;
    const end = template.current?.selectionEnd ?? start;
    setSettings({
      ...settings,
      template: settings.template.slice(0, start) + token + settings.template.slice(end),
    });
  }
  async function say(e?: FormEvent, phrase?: string) {
    e?.preventDefault();
    const value = (phrase ?? quick).trim();
    if (!value) return;
    setBusy('say');
    setError('');
    setMessage('');
    try {
      await api('/desktop/tools/chatbox', { action: 'say', text: value, open: keyboard });
      typing.current = false;
      if (phrase === undefined) setQuick('');
      setRecent([value, ...recent.filter((line) => line !== value)].slice(0, 5));
      setMessage(tr(keyboard ? 'Opened in the VRChat keyboard' : 'Sent to VRChat'));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy('');
    }
  }
  async function savePhrases(phrases: string[]) {
    setError('');
    try {
      const d: ChatboxState = await api('/desktop/tools/chatbox', { action: 'phrases', phrases });
      setRemote(d);
      setSettings({ ...settings, phrases: d.settings.phrases ?? phrases });
    } catch (e) {
      setError((e as Error).message);
    }
  }
  return (
    <section className="dashboard-panel desktop-tool login-content" aria-labelledby="chatbox-title">
      <div className="desktop-tool-heading">
        <MessageSquare size={20} />
        <div>
          <h3 id="chatbox-title">{tr('OSC chatbox')}</h3>
          <p>{tr('Send text to your chatbox in VRChat.')}</p>
        </div>
        <span className={'tool-state' + (remote?.automatic ? ' active' : '')}>
          {tr(remote?.automatic ? 'Automatic' : 'Manual')}
        </span>
      </div>
      <p className="fineprint">
        {tr(
          'In VRChat, open the Action Menu, then OSC, then Enabled. Messages go only to VRChat on this PC.',
        )}
      </p>
      <form className="tool-quick-chat" onSubmit={(e) => void say(e)}>
        <label htmlFor="chatbox-quick">{tr('Say something')}</label>
        <div className="tool-quick-row">
          <input
            id="chatbox-quick"
            value={quick}
            maxLength={chatboxLimit}
            onChange={(e) => {
              setQuick(e.target.value);
              setTyping(!!e.target.value);
            }}
            onBlur={() => setTyping(false)}
          />
          <button className="standard-button" disabled={!!busy || !remote || !quick.trim()}>
            <Send size={14} />
            {tr('Send')}
          </button>
          <button
            type="button"
            className="standard-button"
            aria-label={tr('Save as a phrase')}
            title={tr('Save as a phrase')}
            disabled={!quick.trim() || settings.phrases.includes(quick.trim())}
            onClick={() => void savePhrases([...settings.phrases, quick.trim()])}
          >
            <Plus size={14} />
          </button>
        </div>
        <label className="tool-checkbox">
          <input type="checkbox" checked={keyboard} onChange={(e) => setKeyboard(e.target.checked)} />
          {tr('Open in the VRChat keyboard to edit before sending')}
        </label>
      </form>
      {settings.phrases.length > 0 && <RowHeading title={tr('Saved phrases')} />}
      {settings.phrases.length > 0 && (
        <div className="tool-phrases" aria-label={tr('Saved phrases')}>
          {settings.phrases.map((phrase) => (
            <span className="tool-phrase" key={phrase}>
              <button type="button" disabled={!!busy} onClick={() => void say(undefined, phrase)}>
                {phrase}
              </button>
              <button
                type="button"
                aria-label={tr('Remove {name}', { name: phrase })}
                onClick={() => void savePhrases(settings.phrases.filter((p) => p !== phrase))}
              >
                <X size={12} />
              </button>
            </span>
          ))}
        </div>
      )}
      {recent.length > 0 && (
        <RowHeading
          title={tr('Recent messages')}
          hint={tr('Click one to reuse it. This list clears when you leave this page.')}
        />
      )}
      {recent.length > 0 && (
        <div className="tool-phrases" aria-label={tr('Recent messages')}>
          {recent.map((line) => (
            <span className="tool-phrase recent" key={line}>
              <button type="button" onClick={() => setQuick(line)}>
                {line}
              </button>
            </span>
          ))}
        </div>
      )}
      <RowHeading title={tr('Presets')} hint={tr('Click one to fill in the Message box.')} />
      <div className="result-actions tool-presets" aria-label={tr('Presets')}>
        <button
          className="standard-button"
          disabled={locked}
          onClick={() => setSettings({ ...settings, template: '{time}' })}
        >
          {tr('Time')}
        </button>
        <button
          className="standard-button"
          disabled={locked}
          onClick={() => setSettings({ ...settings, template: tr('AFK: Back soon') })}
        >
          AFK
        </button>
        <button
          className="standard-button"
          disabled={locked || !user?.statusDescription}
          onClick={() =>
            setSettings({ ...settings, template: '{status}', status: user?.statusDescription || '' })
          }
        >
          {tr('My VRChat status')}
        </button>
        <button
          className="standard-button"
          disabled={locked}
          onClick={() => setSettings({ ...settings, template: '{time} | {media}' })}
        >
          {tr('Now playing')}
        </button>
        <button
          className="standard-button"
          disabled={locked}
          onClick={() => setSettings({ ...settings, template: '{cpu} | {gpu} | {ram}' })}
        >
          {tr('System stats')}
        </button>
        <button
          className="standard-button"
          disabled={locked}
          onClick={() => setSettings({ ...settings, template: '{world} ({players})\n{playtime}' })}
        >
          {tr('Current world')}
        </button>
      </div>
      <div className="tool-message-field">
        <label htmlFor="chatbox-message">{tr('Message')}</label>
        <textarea
          id="chatbox-message"
          ref={template}
          rows={3}
          maxLength={2048}
          value={settings.template}
          disabled={locked}
          onChange={(e) => setSettings({ ...settings, template: e.target.value })}
          placeholder={tr('Write a message, or use a preset.')}
        />
      </div>
      <RowHeading
        title={tr('Add a live value')}
        hint={tr('Click one to add it to your message. People see the real value, not the code.')}
      />
      <div className="tool-tokens" aria-label={tr('Add a live value')}>
        {insertable.map(([token, label]) => (
          <button key={token} type="button" disabled={locked} onClick={() => insert(token)}>
            {tr(label)} <code>{token}</code>
          </button>
        ))}
      </div>
      <small className="fineprint">
        {tr(
          'Values update with each message. A line disappears when its values are empty, such as {media} with nothing playing.',
        )}
      </small>
      {settings.template.includes('{status}') && (
        <label>
          {tr('Status text')}
          <input
            value={settings.status}
            disabled={locked}
            onChange={(e) => setSettings({ ...settings, status: e.target.value })}
          />
        </label>
      )}
      <div className="chatbox-preview" aria-label={tr('Message preview')}>
        <span>{text || tr('Your message appears here')}</span>
        <small>
          {text.length}/{chatboxLimit}
        </small>
      </div>
      <details className="tool-options">
        <summary>{tr('Rotating messages and AFK')}</summary>
        <div className="tool-option-fields tool-option-stack">
          <label>
            {tr('Rotating messages, one per line')}
            <textarea
              rows={4}
              disabled={locked}
              value={settings.rotation.join('\n')}
              onChange={(e) =>
                setSettings({
                  ...settings,
                  rotation: e.target.value === '' ? [] : e.target.value.split('\n').slice(0, 30),
                })
              }
            />
          </label>
          <small className="fineprint">
            {tr(
              'How to use: type your messages above, one per line. Add {rotate} to the Message box where they should show. Click Start automatic messages. Each message that sends uses the next line.',
            )}
          </small>
          <label>
            {tr('Message while AFK')}
            <input
              value={settings.afkText}
              disabled={locked}
              maxLength={1024}
              onChange={(e) => setSettings({ ...settings, afkText: e.target.value })}
              placeholder={tr('AFK for {afktime}')}
            />
          </label>
          <small className="fineprint">
            {tr(
              'Replaces your message while VRChat reports you as AFK. Connect under Avatar parameters for VRCove to see this.',
            )}
          </small>
        </div>
      </details>
      <details className="tool-options">
        <summary>{tr('Chatbox options')}</summary>
        <div className="tool-option-fields">
          <label>
            {tr('OSC port')}
            <input
              type="number"
              min={1024}
              max={65535}
              disabled={locked}
              value={settings.port}
              onChange={(e) => setSettings({ ...settings, port: Number(e.target.value) })}
            />
          </label>
          <label>
            {tr('Repeat every (seconds)')}
            <input
              type="number"
              min={5}
              max={300}
              disabled={locked}
              value={settings.interval}
              onChange={(e) => setSettings({ ...settings, interval: Number(e.target.value) })}
            />
          </label>
          <label className="tool-checkbox">
            <input
              type="checkbox"
              checked={settings.sound}
              disabled={locked}
              onChange={(e) => setSettings({ ...settings, sound: e.target.checked })}
            />
            {tr('Play notification sound')}
          </label>
        </div>
      </details>
      {(error || remote?.error) && (
        <p className="form-error" role="alert">
          {error || remote?.error}
        </p>
      )}
      {message && (
        <p className="fineprint" role="status">
          {message}
        </p>
      )}
      <div className="result-actions">
        <button
          className="standard-button"
          disabled={!!busy || !remote || !valid || remote.automatic}
          onClick={() => void act('send')}
        >
          <Send size={14} />
          {tr('Send once')}
        </button>
        {remote?.automatic ? (
          <button className="standard-button" disabled={!!busy} onClick={() => void act('stop')}>
            <Square size={14} />
            {tr('Stop automatic messages')}
          </button>
        ) : (
          <button
            className="standard-button"
            disabled={!!busy || !remote || !settings.template.trim()}
            onClick={() => void act('start')}
          >
            <Play size={14} />
            {tr('Start automatic messages')}
          </button>
        )}
        <button
          className="standard-button"
          disabled={!!busy || !remote || remote.automatic}
          onClick={() => void act('save')}
        >
          {busy && <LoaderCircle size={14} className="spin" />}
          {tr('Save settings')}
        </button>
        <button className="standard-button" disabled={!!busy || !remote} onClick={() => void act('clear')}>
          <Eraser size={14} />
          {tr('Clear chatbox')}
        </button>
      </div>
      <p className="fineprint">
        {tr(
          'Automatic messages continue in the tray. They start off when you reopen the app. Allow at least 5 seconds between messages.',
        )}
      </p>
    </section>
  );
}

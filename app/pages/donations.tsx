import { t as tr, useLocale } from '../lib/i18n';
import { useState } from 'react';
import { Heart, Copy, Check, ArrowUpRight, Bitcoin } from 'lucide-react';

const address = 'bc1qxyqvupkzlyz9704h2cn0adqegrq0evar6878gf';

export default function Donations() {
  useLocale();
  const [copied, setCopied] = useState(false),
    [error, setError] = useState('');
  async function copy() {
    try {
      await navigator.clipboard.writeText(address);
      setCopied(true);
      setError('');
    } catch {
      setError(tr('Select and copy the address below. Clipboard access is unavailable.'));
    }
  }
  return (
    <section className="donations-page" aria-labelledby="donations-title">
      <div className="donations-intro">
        <span className="eyebrow">
          <Heart size={15} />
          {tr('SUPPORT VRCOVE')}
        </span>
        <h1 id="donations-title">{tr('A little support goes a long way.')}</h1>
        <p>
          {tr(
            'If you enjoy using VRCove, you can support the project with a Bitcoin donation. Any amount is appreciated.',
          )}
        </p>
      </div>
      <div className="donation-card">
        <div className="donation-heading">
          <span className="bitcoin-mark">
            <Bitcoin size={28} />
          </span>
          <div>
            <h2>{tr('Donate Bitcoin')}</h2>
            <p>{tr('BTC · Bitcoin network')}</p>
          </div>
        </div>
        <label htmlFor="donation-address">{tr('Bitcoin address')}</label>
        <input
          id="donation-address"
          className="donation-address"
          value={address}
          readOnly
          onFocus={(e) => e.currentTarget.select()}
          spellCheck={false}
        />
        <div className="donation-actions">
          <button className="primary" onClick={copy}>
            {copied ? <Check size={16} /> : <Copy size={16} />} {copied ? 'Address copied' : 'Copy address'}
          </button>
          <a className="standard-button" href={'bitcoin:' + address}>
            {tr('Open Bitcoin wallet')}
            <ArrowUpRight size={16} />
          </a>
        </div>
        <p className="donation-status" role="status">
          {error ||
            (copied
              ? 'Ready to paste into your Bitcoin wallet.'
              : 'Copy the address or open it in your Bitcoin wallet.')}
        </p>
        <div className="donation-note">
          <strong>{tr('Send BTC on the Bitcoin network.')}</strong>
          <p>
            {tr(
              'Check the full address in your wallet before sending. Donations are optional and do not unlock account features.',
            )}
          </p>
        </div>
      </div>
      <p className="donation-thanks">
        {tr('Thank you for supporting VRCove.')}
        <Heart size={14} />
      </p>
    </section>
  );
}

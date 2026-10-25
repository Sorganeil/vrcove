import { t as tr, useLocale } from '../lib/i18n';
import { ShieldCheck, ArrowUpRight } from 'lucide-react';
import { useVRChatSession, type VRChatSessionIssue } from '../lib/api';
import { desktop } from '../lib/platform';
import type { CoveAccount } from '../lib/types';
// Signed-out prompt shared by every page that needs a linked VRChat session.
// A session that expired or was revoked says reconnect; a browser that never connected says connect.
export function gateTitle(coveAccount: Pick<CoveAccount, 'links'> | null, issue: VRChatSessionIssue | null) {
  if (!coveAccount) return tr('Sign in to VRCove and link VRChat');
  if (!coveAccount.links?.length) return tr(desktop ? 'Sign in to VRChat' : 'Link your VRChat account');
  return tr(
    issue === 'vrchat_session_expired' || issue === 'vrchat_session_invalid'
      ? 'Reconnect your VRChat account'
      : 'Connect VRChat on this device',
  );
}
export default function VRChatGate({
  coveAccount,
  section,
  onConnect,
  size = 30,
}: {
  coveAccount: Pick<CoveAccount, 'links'> | null;
  section: string;
  onConnect: () => void;
  size?: number;
}) {
  useLocale();
  const { issue } = useVRChatSession();
  return (
    <div className="empty-state">
      <ShieldCheck size={size} />
      <h3>{gateTitle(coveAccount, issue)}</h3>
      <p>
        {tr(
          desktop
            ? 'Sign in to VRChat to view {section} and use VRChat actions. The app talks to VRChat directly.'
            : coveAccount
              ? 'Connect VRChat to view {section} and use VRChat actions. Your VRCove account stays signed in.'
              : 'Sign in to VRCove, then link VRChat to view {section} and use account actions.',
          { section },
        )}
        <br />
        {tr('Your password is never saved.')}
      </p>
      <button className="primary" onClick={onConnect}>
        {tr(desktop ? 'Sign in to VRChat' : 'Connect VRChat')}
        <ArrowUpRight size={16} />
      </button>
    </div>
  );
}

import { useState } from 'react';
import { LogOut, UserRound } from 'lucide-react';
import { api, resetVRChatSession } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import ProfileEditor from '../components/profile-editor';
import { DetailView } from '../components/vrchat-details';
import { RequireVRChat, SectionPage } from '../components/section-page';

/** The connected VRChat account's own profile, with editing and disconnect. */
export default function MyProfile() {
  return (
    <SectionPage title="My profile" subtitle="YOUR VRCHAT PROFILE">
      <RequireVRChat section="Profile">
        <OwnProfile />
      </RequireVRChat>
    </SectionPage>
  );
}

function OwnProfile() {
  useLocale();
  const { user, setUser, coveAccount, requestConnection, notify, navigate } = useApp();
  const [editing, setEditing] = useState(false);
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState(false);

  async function disconnect() {
    setBusy(true);
    try {
      await api('/account/logout', {});
      resetVRChatSession();
      setUser(null);
      navigate('Explore');
      notify(tr('Disconnected from VRChat'));
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="my-vrchat-profile">
      <div className="profile-actions">
        <button className="primary" onClick={() => setEditing(true)}>
          {tr('Edit profile')}
          <UserRound size={14} />
        </button>
        <button onClick={disconnect} disabled={busy}>
          {tr('Disconnect')}
          <LogOut size={15} />
        </button>
      </div>
      <DetailView
        key={user.id + ':' + revision}
        coveAccount={coveAccount}
        kind="user"
        id={user.id}
        user={user}
        onLogin={() => requestConnection()}
        notify={notify}
        mode="page"
      />
      {editing && (
        <ProfileEditor
          user={user}
          close={() => setEditing(false)}
          updated={(u: any) => {
            setUser(u);
            setRevision((r) => r + 1);
            setEditing(false);
            notify(tr('Profile updated in VRChat'));
          }}
        />
      )}
    </div>
  );
}

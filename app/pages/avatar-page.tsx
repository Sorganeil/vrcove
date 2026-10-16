import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { useLoader } from '../lib/use-loader';
import { normalize, type Avatar } from '../lib/avatar';
import AvatarView from '../components/avatar-view';
import Loading from '../components/loading';

/** Full avatar page (/avatar/<id>): the catalog record, or a live lookup through the linked VRChat session. */
export default function AvatarPage({ id }: { id: string }) {
  useLocale();
  const { user } = useApp();
  const {
    data: avatar,
    loading,
    error,
  } = useLoader<Avatar | null>(
    async () => {
      const found = await api('/catalog?q=' + encodeURIComponent(id));
      if (found.avatars[0]) return found.avatars[0] as Avatar;
      if (!user)
        throw new Error(
          'This avatar isn’t in the public catalog. Connect your VRChat account to look it up live.',
        );
      return normalize(await api('/account/avatar?id=' + id));
    },
    [id, user?.id],
    null,
  );
  if (avatar?.id === id && !loading) return <AvatarView avatar={avatar} page />;
  if (error)
    return (
      <div className="entity-page">
        <div className="error-banner" role="alert">
          {error}
        </div>
      </div>
    );
  return <Loading text={tr('Loading avatar…')} />;
}

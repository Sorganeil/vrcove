import { t as tr } from '../lib/i18n';
import { useState } from 'react';
export function CovePicture({
  picture,
  username,
  size = 32,
}: {
  picture?: string;
  username: string;
  size?: number;
}) {
  const [failed, setFailed] = useState('');
  const style = { width: size, height: size, flexShrink: 0 };
  return picture && failed !== picture ? (
    <img
      className="cove-account-picture"
      style={style}
      src={picture}
      alt={tr('VRCove profile picture')}
      onError={() => setFailed(picture)}
    />
  ) : (
    <span
      className="cove-account-picture"
      style={style}
      aria-hidden="true"
      data-initial={[...username][0]?.toUpperCase()}
    />
  );
}

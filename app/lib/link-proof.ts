import { api } from './api';

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Links the VRChat account signed in through the VRChat worker to the VRCove
 * account. VRCove's server never sees that session, so the worker puts a
 * one-time code in the VRChat bio, the server's bot reads it from the public
 * profile, and the worker then restores the bio.
 */
export async function proveVRChatLink(vrchatId: string, username?: string) {
  const { code } = await api('/cove/link/challenge', { vrchatId });
  const me = await api('/account/me');
  if (me.id !== vrchatId) throw new Error('Switch to that VRChat account first.');
  const original: string = me.bio || '';
  const withCode = original ? `${original.slice(0, 512 - code.length - 1)}\n${code}` : code;
  await api('/account/profile', { bio: withCode });
  try {
    // VRChat can take a moment to show the new bio to other accounts.
    for (let attempt = 0; ; attempt++) {
      try {
        return await api('/cove/link/verify', { vrchatId, username });
      } catch (e) {
        if (attempt >= 3 || (e as { status?: number }).status !== 409) throw e;
        await wait(3000);
      }
    }
  } finally {
    await api('/account/profile', { bio: original }).catch(() => undefined);
  }
}

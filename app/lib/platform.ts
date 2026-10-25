/**
 * True in the desktop app, which is built from this same frontend with
 * `vite build --mode desktop`. There VRChat requests go straight from the
 * device to VRChat, and a VRCove account is optional.
 */
export const desktop = import.meta.env.MODE === 'desktop';

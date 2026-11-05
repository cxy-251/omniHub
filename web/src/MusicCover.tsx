import { useState } from 'react';

export const audioCoverURL = (path: string) => `/api/media/audio/cover?path=${encodeURIComponent(path)}`;

/** Actual library artwork, with a neutral fallback when missing or undecodable. */
export default function MusicCover({ path, className = '' }: { path?: string; className?: string }) {
  const [failed, setFailed] = useState('');
  return <span className={`music-cover ${className}`}>
    {path && failed !== path ? <img src={audioCoverURL(path)} loading="lazy" decoding="async" alt="" onError={() => setFailed(path)} />
      : <svg viewBox="0 0 80 80" width="64" height="64" fill="none" stroke="currentColor" strokeWidth="2" aria-label="暂无封面" role="img"><circle cx="40" cy="40" r="28" /><circle cx="40" cy="40" r="7" /><path d="M20 40a20 20 0 0 1 20-20M60 40a20 20 0 0 1-20 20" /></svg>}
  </span>;
}

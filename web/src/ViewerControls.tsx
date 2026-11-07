import type { ReactNode } from 'react';

const baseBtnClass =
  'group relative flex h-9 w-9 shrink-0 items-center justify-center rounded-full border border-white/15 bg-white/10 text-white/90 backdrop-blur-md transition hover:border-white/35 hover:bg-white/20 hover:text-white active:scale-95 disabled:pointer-events-none disabled:opacity-35';

const activeBtnClass =
  'border-primary/70 bg-primary/25 text-primary hover:border-primary hover:bg-primary/35';

const dangerBtnClass =
  'border-[#ff7b72]/80 bg-[#ff7b72]/25 text-[#ff7b72] hover:bg-[#ff7b72]/35';

const bubbleClass =
  'pointer-events-none absolute top-full right-0 mt-1.5 z-30 whitespace-nowrap rounded-md bg-black/90 px-2 py-1 text-xs font-normal text-white opacity-0 shadow-lg ring-1 ring-white/15 transition-opacity duration-150 group-hover:opacity-100';

export function ViewerIconButton({
  label,
  active = false,
  danger = false,
  disabled = false,
  onClick,
  children,
}: {
  label: string;
  active?: boolean;
  danger?: boolean;
  disabled?: boolean;
  onClick?: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className={`${baseBtnClass} ${danger ? dangerBtnClass : active ? activeBtnClass : ''}`}
    >
      {children}
      <span className={bubbleClass}>{label}</span>
    </button>
  );
}

export function ViewerIconLink({
  label,
  href,
  download,
  children,
}: {
  label: string;
  href: string;
  download?: string;
  children: ReactNode;
}) {
  return (
    <a
      aria-label={label}
      href={href}
      download={download}
      className={baseBtnClass}
    >
      {children}
      <span className={bubbleClass}>{label}</span>
    </a>
  );
}

const Svg = ({ children, fill = 'none' }: { children: ReactNode; fill?: string }) => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill={fill} stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {children}
  </svg>
);

export const IconHeart = ({ filled }: { filled: boolean }) => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 24 24"
    fill={filled ? '#f778ba' : 'none'}
    stroke={filled ? '#f778ba' : 'currentColor'}
    strokeWidth="1.9"
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
  >
    <path d="M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.3 1.5 4.05 3 5.5l7 7Z" />
  </svg>
);

export const IconTrash = () => (
  <Svg>
    <path d="M3 6h18" />
    <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
    <path d="M10 11v6M14 11v6" />
  </Svg>
);

export const IconRestore = () => (
  <Svg>
    <path d="M3 7v6h6" />
    <path d="M21 17a9 9 0 0 0-15-6.7L3 13" />
  </Svg>
);

export const IconDownload = () => (
  <Svg>
    <path d="M12 3v12" />
    <path d="m7 10 5 5 5-5" />
    <path d="M5 21h14" />
  </Svg>
);

export const IconFill = () => (
  <Svg>
    <rect x="3" y="5" width="18" height="14" rx="2" />
    <path d="M7 9h10v6H7z" />
  </Svg>
);

export const IconFit = () => (
  <Svg>
    <rect x="4" y="4" width="16" height="16" rx="2" />
    <path d="M4 9h16M4 15h16" />
  </Svg>
);

export const IconFullscreen = () => (
  <Svg>
    <path d="M8 3H5a2 2 0 0 0-2 2v3" />
    <path d="M21 8V5a2 2 0 0 0-2-2h-3" />
    <path d="M3 16v3a2 2 0 0 0 2 2h3" />
    <path d="M16 21h3a2 2 0 0 0 2-2v-3" />
  </Svg>
);

export const IconExitFullscreen = () => (
  <Svg>
    <path d="M8 3v3a2 2 0 0 1-2 2H3" />
    <path d="M21 8h-3a2 2 0 0 1-2-2V3" />
    <path d="M3 16h3a2 2 0 0 1 2 2v3" />
    <path d="M16 21v-3a2 2 0 0 1 2-2h3" />
  </Svg>
);

export const IconPrev = () => (
  <Svg>
    <path d="m15 18-6-6 6-6" />
  </Svg>
);

export const IconNext = () => (
  <Svg>
    <path d="m9 18 6-6-6-6" />
  </Svg>
);

export const IconPlay = () => (
  <Svg fill="currentColor">
    <polygon points="6 4 20 12 6 20 6 4" />
  </Svg>
);

export const IconPause = () => (
  <Svg fill="currentColor">
    <rect x="6" y="4" width="4" height="16" rx="1" />
    <rect x="14" y="4" width="4" height="16" rx="1" />
  </Svg>
);

export const IconVolume = () => (
  <Svg>
    <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
    <path d="M15.54 8.46a5 5 0 0 1 0 7.07" />
    <path d="M19.07 4.93a10 10 0 0 1 0 14.14" />
  </Svg>
);

export const IconMute = () => (
  <Svg>
    <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
    <line x1="23" y1="9" x2="17" y2="15" />
    <line x1="17" y1="9" x2="23" y2="15" />
  </Svg>
);

export const IconClose = () => (
  <Svg>
    <line x1="18" y1="6" x2="6" y2="18" />
    <line x1="6" y1="6" x2="18" y2="18" />
  </Svg>
);

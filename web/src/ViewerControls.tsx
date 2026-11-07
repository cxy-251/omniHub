import type { ReactNode } from 'react';

const baseBtnClass =
  'group relative inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-full border border-white/15 bg-white/10 text-white/90 backdrop-blur-md transition hover:border-white/35 hover:bg-white/20 hover:text-white active:scale-95 disabled:pointer-events-none disabled:opacity-35';

const activeBtnClass =
  'border-primary/70 bg-primary/25 text-primary hover:border-primary hover:bg-primary/35';

const primaryBtnClass =
  'border-primary bg-primary text-[#0f241d] hover: opacity-95 shadow-md';

const dangerBtnClass =
  'border-[#ff7b72]/80 bg-[#ff7b72]/25 text-[#ff7b72] hover:bg-[#ff7b72]/35';

const bubbleDownClass =
  'pointer-events-none absolute top-full right-0 mt-1.5 z-40 whitespace-nowrap rounded-md bg-black/90 px-2 py-1 text-xs font-normal text-white opacity-0 shadow-lg ring-1 ring-white/15 transition-opacity duration-150 group-hover:opacity-100';

const bubbleUpClass =
  'pointer-events-none absolute bottom-full left-1/2 -translate-x-1/2 mb-1.5 z-40 whitespace-nowrap rounded-md bg-black/90 px-2 py-1 text-xs font-normal text-white opacity-0 shadow-lg ring-1 ring-white/15 transition-opacity duration-150 group-hover:opacity-100';

export function ViewerIconButton({
  label,
  active = false,
  primary = false,
  danger = false,
  disabled = false,
  size = 'md',
  tooltip = 'down',
  badge,
  onClick,
  children,
}: {
  label: string;
  active?: boolean;
  primary?: boolean;
  danger?: boolean;
  disabled?: boolean;
  size?: 'sm' | 'md' | 'lg';
  tooltip?: 'up' | 'down';
  badge?: string;
  onClick?: (e: React.MouseEvent<HTMLButtonElement>) => void;
  children: ReactNode;
}) {
  const sizeClass = size === 'sm' ? 'h-7 w-7' : size === 'lg' ? 'h-12 w-12' : 'h-9 w-9';
  const stateClass = danger ? dangerBtnClass : primary ? primaryBtnClass : active ? activeBtnClass : '';
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className={`${baseBtnClass} ${sizeClass} ${stateClass}`}
    >
      {children}
      {badge ? (
        <span className="pointer-events-none absolute -bottom-0.5 -right-0.5 rounded-full bg-black/85 px-1 text-[9px] font-semibold leading-tight text-primary ring-1 ring-white/20">
          {badge}
        </span>
      ) : null}
      <span className={tooltip === 'up' ? bubbleUpClass : bubbleDownClass}>{label}</span>
    </button>
  );
}

export function ViewerIconLink({
  label,
  href,
  download,
  tooltip = 'down',
  children,
}: {
  label: string;
  href: string;
  download?: string;
  tooltip?: 'up' | 'down';
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
      <span className={tooltip === 'up' ? bubbleUpClass : bubbleDownClass}>{label}</span>
    </a>
  );
}

export function VolumeSliderPill({
  volume,
  muted,
  onToggleMute,
  onChangeVolume,
  tooltip = 'down',
}: {
  volume: number;
  muted: boolean;
  onToggleMute: () => void;
  onChangeVolume: (v: number) => void;
  tooltip?: 'up' | 'down';
}) {
  const effective = muted ? 0 : Math.max(0, Math.min(1, volume));
  const pct = Math.round(effective * 100);
  return (
    <div
      className="group relative inline-flex h-9 shrink-0 items-center gap-1.5 rounded-full border border-white/15 bg-white/10 pl-1.5 pr-2.5 text-white/90 backdrop-blur-md transition hover:border-white/35 hover:bg-white/15"
      onWheel={e => {
        e.stopPropagation();
        const delta = e.deltaY < 0 ? 0.05 : -0.05;
        onChangeVolume(Math.max(0, Math.min(1, Number((effective + delta).toFixed(2)))));
      }}
    >
      <button
        type="button"
        aria-label={muted || effective === 0 ? '取消静音' : '静音'}
        onClick={onToggleMute}
        className="flex h-6 w-6 items-center justify-center rounded-full text-white/90 transition hover:bg-white/15 hover:text-white"
      >
        {muted || effective === 0 ? <IconMute /> : <IconVolume />}
      </button>
      <input
        type="range"
        aria-label="音量大小"
        min={0}
        max={1}
        step={0.02}
        value={effective}
        onChange={e => onChangeVolume(Number(e.target.value))}
        className="h-1 w-16 cursor-pointer accent-primary sm:w-20"
      />
      <span className={tooltip === 'up' ? bubbleUpClass : bubbleDownClass}>
        {muted || effective === 0 ? '已静音 (点击恢复 / 滚轮调节)' : `音量 ${pct}% (点击静音 / 滚轮调节)`}
      </span>
    </div>
  );
}

const Svg = ({ children, fill = 'none', size = 18 }: { children: ReactNode; fill?: string; size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill={fill} stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
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

export const IconThumbDown = ({ filled }: { filled?: boolean }) => (
  <Svg fill={filled ? 'currentColor' : 'none'}>
    <path d="M17 14V2" />
    <path d="M9 18.12 10 14H4.17a2 2 0 0 1-1.92-2.56l2.33-8A2 2 0 0 1 6.5 2H20a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-2.76a2 2 0 0 0-1.79 1.11L12 22a3.13 3.13 0 0 1-3-3.88Z" />
  </Svg>
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

export const IconSkipBack = () => (
  <Svg>
    <polygon points="19 20 9 12 19 4 19 20" fill="currentColor" />
    <line x1="5" y1="19" x2="5" y2="5" />
  </Svg>
);

export const IconSkipForward = () => (
  <Svg>
    <polygon points="5 4 15 12 5 20 5 4" fill="currentColor" />
    <line x1="19" y1="5" x2="19" y2="19" />
  </Svg>
);

export const IconRewind10 = () => (
  <Svg>
    <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
    <path d="M3 3v5h5" />
    <path d="M10 10v5M14 10v5" />
  </Svg>
);

export const IconForward30 = () => (
  <Svg>
    <path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8" />
    <path d="M21 3v5h-5" />
    <path d="M10 10h4v5h-4" />
  </Svg>
);

export const IconRepeatList = () => (
  <Svg>
    <path d="m17 2 4 4-4 4" />
    <path d="M3 11v-1a4 4 0 0 1 4-4h14" />
    <path d="m7 22-4-4 4-4" />
    <path d="M21 13v1a4 4 0 0 1-4 4H3" />
  </Svg>
);

export const IconRepeatOne = () => (
  <Svg>
    <path d="m17 2 4 4-4 4" />
    <path d="M3 11v-1a4 4 0 0 1 4-4h14" />
    <path d="m7 22-4-4 4-4" />
    <path d="M21 13v1a4 4 0 0 1-4 4H3" />
    <path d="M11 10h1v4" />
  </Svg>
);

export const IconShuffle = () => (
  <Svg>
    <path d="M2 18h1.4c1.3 0 2.5-.6 3.3-1.7l6.1-8.6c.7-1.1 2-1.7 3.3-1.7H22" />
    <path d="m18 2 4 4-4 4" />
    <path d="M2 6h1.9c1.5 0 2.9.9 3.6 2.2" />
    <path d="M22 18h-5.9c-1.3 0-2.6-.7-3.3-1.8l-.5-.8" />
    <path d="m18 14 4 4-4 4" />
  </Svg>
);

export const IconSpeed = () => (
  <Svg>
    <path d="M12 14 15.5 9.5" />
    <path d="M20.38 18.5A9 9 0 1 0 3.62 18.5" />
  </Svg>
);

export const IconSleep = () => (
  <Svg>
    <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
  </Svg>
);

export const IconStop = () => (
  <Svg fill="currentColor">
    <rect x="6" y="6" width="12" height="12" rx="2" />
  </Svg>
);

export const IconMore = () => (
  <Svg>
    <circle cx="12" cy="12" r="1.5" fill="currentColor" />
    <circle cx="19" cy="12" r="1.5" fill="currentColor" />
    <circle cx="5" cy="12" r="1.5" fill="currentColor" />
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

export const IconMotion = () => (
  <Svg>
    <path d="M5 12h14" />
    <path d="m15 8 4 4-4 4" />
    <path d="m9 8-4 4 4 4" />
  </Svg>
);


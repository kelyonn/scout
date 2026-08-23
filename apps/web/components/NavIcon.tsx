// Small hand-drawn line icons for Shell's sidebar nav, replacing the
// 01-08 numeric index. Same stroke weight and rounded joins as the
// collapse-toggle chevron already in Shell.tsx, so they read as one
// icon family rather than a mismatched set pulled from a library.
const ICON_PATHS: Record<string, React.ReactNode> = {
  home: (
    <>
      <path d="M3.5 9.5L10 4l6.5 5.5" />
      <path d="M5.2 8.3V16a.7.7 0 0 0 .7.7h3V13a1 1 0 0 1 1-1h.2a1 1 0 0 1 1 1v3.7h3a.7.7 0 0 0 .7-.7V8.3" />
    </>
  ),
  opportunities: (
    <>
      <circle cx="10" cy="10" r="6.5" />
      <circle cx="10" cy="10" r="3" />
      <circle cx="10" cy="10" r="0.6" fill="currentColor" stroke="none" />
    </>
  ),
  saved: <path d="M6 3.5h8a1 1 0 0 1 1 1V17l-5-3.3L5 17V4.5a1 1 0 0 1 1-1z" />,
  applied: (
    <>
      <path d="M17 3L3 9.3l5.3 2 2 5.4L17 3z" />
      <path d="M8.3 11.3L17 3" />
    </>
  ),
  pipeline: (
    <>
      <rect x="3" y="5" width="3.6" height="11" rx="1" />
      <rect x="8.2" y="8" width="3.6" height="8" rx="1" />
      <rect x="13.4" y="3.5" width="3.6" height="12.5" rx="1" />
    </>
  ),
  search: (
    <>
      <circle cx="8.5" cy="8.5" r="5" />
      <line x1="12.3" y1="12.3" x2="16.5" y2="16.5" />
    </>
  ),
  calendar: (
    <>
      <rect x="3" y="4.5" width="14" height="12" rx="1.5" />
      <line x1="3" y1="8.3" x2="17" y2="8.3" />
      <line x1="6.8" y1="2.5" x2="6.8" y2="5.5" />
      <line x1="13.2" y1="2.5" x2="13.2" y2="5.5" />
    </>
  ),
  companies: (
    <>
      <rect x="4" y="3" width="12" height="14" rx="1" />
      <line x1="7" y1="6.7" x2="7" y2="6.7" strokeWidth="2.2" />
      <line x1="10" y1="6.7" x2="10" y2="6.7" strokeWidth="2.2" />
      <line x1="13" y1="6.7" x2="13" y2="6.7" strokeWidth="2.2" />
      <line x1="7" y1="10.3" x2="7" y2="10.3" strokeWidth="2.2" />
      <line x1="13" y1="10.3" x2="13" y2="10.3" strokeWidth="2.2" />
      <line x1="8.6" y1="17" x2="8.6" y2="13.6" />
      <line x1="11.4" y1="17" x2="11.4" y2="13.6" />
    </>
  ),
  insights: (
    <>
      <polyline points="4,3 4,17 17,17" />
      <rect x="6.5" y="11" width="2.4" height="6" rx="0.5" />
      <rect x="10.4" y="7.5" width="2.4" height="9.5" rx="0.5" />
      <rect x="14.3" y="9.5" width="2.4" height="7.5" rx="0.5" />
    </>
  ),
  resume: (
    <>
      <path d="M6.5 3h5l3 3v10.5a1 1 0 0 1-1 1h-7a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z" />
      <path d="M11.5 3v3h3" />
      <line x1="8" y1="10.2" x2="12" y2="10.2" />
      <line x1="8" y1="12.8" x2="12" y2="12.8" />
    </>
  ),
  // A proper six-lobed cog (Heroicons-style silhouette), traced as one
  // closed path rather than the previous radial-spoke asterisk. Authored
  // at a 24x24 grid and scaled 0.8333 (=20/24) via the <g> below so it
  // drops into this file's shared 20x20 viewBox without redoing the
  // math by hand; strokeWidth is scaled up by the inverse (1.5/0.8333)
  // so the rendered line weight still matches its sibling icons.
  settings: (
    <g transform="scale(0.8333)" strokeWidth="1.8">
      <path d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.24-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.752.43.992l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.02-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.992a7.696 7.696 0 0 1 0-.255c.007-.378-.138-.752-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.087.22-.128.332-.183.582-.495.644-.869l.214-1.28Z" />
      <path d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
    </g>
  ),
};

export function NavIcon({ name, className }: { name: string; className?: string }) {
  const path = ICON_PATHS[name];
  if (!path) return null;
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
    >
      {path}
    </svg>
  );
}

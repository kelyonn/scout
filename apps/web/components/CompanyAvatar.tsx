// Extracted from JobCard.tsx so the Companies pages can use the same
// visual identity per company instead of inventing a second scheme.

// A stable color per company, hashed from its id (not name, which two
// different companies can share fragments of) against the theme's own
// existing accent tokens rather than a hardcoded palette, so it stays in
// step with any future retheme.
const AVATAR_COLORS = [
  "var(--accent-primary)",
  "var(--success)",
  "var(--info)",
  "var(--warning)",
  "var(--danger)",
];

function avatarColor(companyId: string): string {
  let hash = 0;
  for (let i = 0; i < companyId.length; i++) hash = (hash * 31 + companyId.charCodeAt(i)) | 0;
  return AVATAR_COLORS[Math.abs(hash) % AVATAR_COLORS.length];
}

export function CompanyAvatar({
  id,
  name,
  size = 34,
}: {
  id: string;
  name: string;
  size?: number;
}) {
  const color = avatarColor(id);
  return (
    <div
      className="shrink-0 rounded-lg flex items-center justify-center font-semibold"
      style={{
        width: size,
        height: size,
        fontSize: size * 0.4,
        color,
        background: `color-mix(in srgb, ${color} 16%, transparent)`,
      }}
    >
      {name.charAt(0).toUpperCase()}
    </div>
  );
}

// docs/12-frontend-ux.md section 2's score band colors — the one
// deliberate exception to the single-accent rule, since score is the
// primary scanning dimension across a list of job cards.
function bandColor(score: number): string {
  if (score >= 90) return "var(--score-exceptional)";
  if (score >= 80) return "var(--score-strong)";
  if (score >= 70) return "var(--score-good)";
  if (score >= 60) return "var(--score-moderate)";
  return "var(--score-weak)";
}

const SIZE_CLASSES = {
  md: "w-11 h-11 text-heading",
  sm: "w-9 h-9 text-label",
} as const;

export function ScoreBadge({
  score,
  size = "md",
}: {
  score: number;
  size?: keyof typeof SIZE_CLASSES;
}) {
  const color = bandColor(score);
  return (
    <div
      className={`flex items-center justify-center rounded-[var(--radius-card)] font-mono tabular-nums shrink-0 ${SIZE_CLASSES[size]}`}
      style={{
        color,
        background: `color-mix(in srgb, ${color} 14%, transparent)`,
        border: `1px solid color-mix(in srgb, ${color} 30%, transparent)`,
      }}
    >
      {score}
    </div>
  );
}

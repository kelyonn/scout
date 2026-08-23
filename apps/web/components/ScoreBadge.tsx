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
  md: "text-xl",
  sm: "text-base",
} as const;

export function ScoreBadge({
  score,
  size = "md",
}: {
  score: number;
  size?: keyof typeof SIZE_CLASSES;
}) {
  return (
    <div
      className={`font-mono font-semibold tabular-nums leading-none shrink-0 ${SIZE_CLASSES[size]}`}
      style={{ color: bandColor(score) }}
    >
      {score}
    </div>
  );
}

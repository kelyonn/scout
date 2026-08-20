// docs/12-frontend-ux.md section 4.3: every subscore rendered with its
// own bar, always — "a composite number without its components is
// unfalsifiable." A missing (null) score renders as an explicit
// "not computed" state rather than being silently omitted, so a
// placeholder-50 subscore this pass never populates (engineering_culture,
// growth_potential, interview_probability) reads as honestly unknown, not
// as a real zero.
export function ScoreRow({ label, value }: { label: string; value?: number | null }) {
  return (
    <div className="flex items-center gap-3">
      <span className="text-label text-text-secondary w-36 shrink-0">{label}</span>
      {value === undefined || value === null ? (
        <span className="text-caption text-text-muted">not computed</span>
      ) : (
        <>
          <div className="flex-1 h-1.5 rounded-full bg-bg-elevated overflow-hidden">
            <div
              className="h-full rounded-full bg-accent-primary transition-[width] duration-250"
              style={{ width: `${value}%` }}
            />
          </div>
          <span className="text-label font-mono tabular-nums text-text-primary w-8 text-right">
            {value}
          </span>
        </>
      )}
    </div>
  );
}

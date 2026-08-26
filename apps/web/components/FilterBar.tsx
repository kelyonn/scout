"use client";

import { useState } from "react";
import {
  LOCATION_TIER_LABEL,
  LOCATION_TIER_OPTIONS,
  ROLE_FAMILY_OPTIONS,
  SENIORITY_LABEL,
  SENIORITY_OPTIONS,
  WORK_MODE_LABEL,
  WORK_MODE_OPTIONS,
  formatRoleFamily,
} from "@/lib/taxonomy";
import type { JobFilters } from "@/lib/useJobFilters";

function toggle(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
}

function CheckboxGroup({
  options,
  labels,
  selected,
  onChange,
}: {
  options: string[];
  labels: Record<string, string>;
  selected: string[];
  onChange: (next: string[]) => void;
}) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((opt) => {
        const active = selected.includes(opt);
        return (
          <button
            key={opt}
            type="button"
            onClick={() => onChange(toggle(selected, opt))}
            aria-pressed={active}
            className={`text-caption rounded-full border px-2.5 py-1 transition-colors duration-100 ${
              active
                ? "border-accent-primary bg-accent-primary/15 text-accent-primary"
                : "border-border-subtle bg-bg-surface text-text-secondary hover:border-border-strong hover:text-text-primary"
            }`}
          >
            {labels[opt] ?? opt}
          </button>
        );
      })}
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-2">
      <div className="text-caption text-text-muted uppercase tracking-wide">{title}</div>
      {children}
    </div>
  );
}

// FilterBar is the concrete implementation of docs/12-frontend-ux.md
// section 4.4's "filter rail" — a plain panel, not the virtualized-list
// version that spec also describes (5,000-row TanStack Virtual list,
// keyboard triage) — that's a separate, much larger feature this pass
// doesn't build. State lives entirely in the URL via useJobFilters, so
// this component is a pure controlled view over `filters`/`onChange`.
export type FilterFacet = "seniority" | "workMode" | "locationTier" | "roleFamily" | "minPriority";

const ALL_FACETS: FilterFacet[] = ["seniority", "workMode", "locationTier", "roleFamily", "minPriority"];

export function FilterBar({
  filters,
  onChange,
  resultCount,
  facets = ALL_FACETS,
}: {
  filters: JobFilters;
  onChange: (next: JobFilters) => void;
  resultCount?: number;
  /** Which facets to render — GET /v1/search doesn't support
   * location_tier or min_priority (packages/db/queries/search.sql only
   * added role_family/seniority/work_mode), so the Search page passes a
   * narrower list rather than showing controls that would silently do
   * nothing. */
  facets?: FilterFacet[];
}) {
  const has = (f: FilterFacet) => facets.includes(f);
  const [expanded, setExpanded] = useState(true);
  const activeCount =
    (has("seniority") ? filters.seniority.length : 0) +
    (has("roleFamily") ? filters.roleFamily.length : 0) +
    (has("workMode") ? filters.workMode.length : 0) +
    (has("locationTier") ? filters.locationTier.length : 0) +
    (has("minPriority") && filters.minPriority !== null ? 1 : 0);

  return (
    <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface mb-6">
      <div className="flex items-center justify-between px-4 py-3">
        <button
          type="button"
          onClick={() => setExpanded((e) => !e)}
          className="flex items-center gap-2 text-label text-text-primary"
        >
          <span className={`transition-transform duration-150 ${expanded ? "rotate-90" : ""}`}>&#8250;</span>
          Filters
          {activeCount > 0 && (
            <span className="text-caption rounded-full bg-accent-primary/15 text-accent-primary px-2 py-0.5">
              {activeCount}
            </span>
          )}
        </button>
        <div className="flex items-center gap-3">
          {resultCount !== undefined && (
            <span className="text-caption text-text-muted">{resultCount} shown</span>
          )}
          {activeCount > 0 && (
            <button
              type="button"
              onClick={() =>
                onChange({ seniority: [], roleFamily: [], workMode: [], locationTier: [], minPriority: null })
              }
              className="text-caption text-text-secondary hover:text-text-primary transition-colors duration-100"
            >
              Clear all
            </button>
          )}
        </div>
      </div>

      {expanded && (
        <div className="px-4 pb-4 pt-1 space-y-4 border-t border-border-subtle">
          {has("seniority") && (
            <Section title="Seniority">
              <CheckboxGroup
                options={SENIORITY_OPTIONS}
                labels={SENIORITY_LABEL}
                selected={filters.seniority}
                onChange={(seniority) => onChange({ ...filters, seniority })}
              />
            </Section>
          )}

          {has("workMode") && (
            <Section title="Work mode">
              <CheckboxGroup
                options={WORK_MODE_OPTIONS}
                labels={WORK_MODE_LABEL}
                selected={filters.workMode}
                onChange={(workMode) => onChange({ ...filters, workMode })}
              />
            </Section>
          )}

          {has("locationTier") && (
            <Section title="Location">
              <CheckboxGroup
                options={LOCATION_TIER_OPTIONS}
                labels={LOCATION_TIER_LABEL}
                selected={filters.locationTier}
                onChange={(locationTier) => onChange({ ...filters, locationTier })}
              />
            </Section>
          )}

          {has("roleFamily") && (
            <Section title="Role">
              <div className="max-h-40 overflow-y-auto pr-1">
                <CheckboxGroup
                  options={ROLE_FAMILY_OPTIONS}
                  labels={Object.fromEntries(ROLE_FAMILY_OPTIONS.map((r) => [r, formatRoleFamily(r)]))}
                  selected={filters.roleFamily}
                  onChange={(roleFamily) => onChange({ ...filters, roleFamily })}
                />
              </div>
            </Section>
          )}

          {has("minPriority") && (
            <Section title="Minimum priority">
              <div className="flex items-center gap-3">
                <input
                  type="range"
                  min={0}
                  max={100}
                  step={1}
                  value={filters.minPriority ?? 0}
                  onChange={(e) => {
                    const v = Number(e.target.value);
                    onChange({ ...filters, minPriority: v === 0 ? null : v });
                  }}
                  className="flex-1 accent-accent-primary"
                />
                <span className="text-caption font-mono tabular-nums text-text-secondary w-8 text-right">
                  {filters.minPriority ?? 0}
                </span>
              </div>
            </Section>
          )}
        </div>
      )}
    </div>
  );
}

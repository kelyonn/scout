import "server-only";
import { readFile } from "node:fs/promises";
import path from "node:path";

// The user's own profile, shown on /profile for quick reference. It lives
// in a gitignored local file (apps/web/.local/profile.json, or wherever
// SCOUT_PROFILE_FILE points) because the repository is public and this is
// personal data — docs/13-security-privacy.md section 3. It is read on the
// server only, never logged, and never sent to a model.

export type Profile = {
  name: string;
  headline?: string;
  location?: string;
  contact: { label: string; value: string }[];
  summary?: string;
  education: { school: string; degree: string; place?: string; period?: string; coursework?: string[] }[];
  experience: { role: string; org: string; place?: string; period?: string; bullets: string[] }[];
  projects: { name: string; stack?: string; status?: string; bullets: string[] }[];
  skills: { group: string; items: string[] }[];
  leadership: { role: string; org: string; period?: string; bullets: string[] }[];
  achievements: string[];
  certifications: string[];
  memberships: string[];
  sources: { label: string; path: string }[];
};

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function list<T>(v: unknown, ok: (x: unknown) => x is T): T[] {
  return Array.isArray(v) ? v.filter(ok) : [];
}

const isStr = (x: unknown): x is string => typeof x === "string";
const str = (v: unknown): string | undefined => (isStr(v) && v.length > 0 ? v : undefined);
const strs = (v: unknown): string[] => list(v, isStr);

// Narrows an unknown JSON value to Profile, dropping anything malformed
// rather than throwing, so a hand-edit typo hides one entry instead of
// taking the page down.
function parse(raw: unknown): Profile | null {
  if (!isRecord(raw) || !isStr(raw.name)) return null;
  const rows = (v: unknown) => list(v, isRecord);
  return {
    name: raw.name,
    headline: str(raw.headline),
    location: str(raw.location),
    contact: rows(raw.contact).flatMap((c) => (isStr(c.label) && isStr(c.value) ? [{ label: c.label, value: c.value }] : [])),
    summary: str(raw.summary),
    education: rows(raw.education).flatMap((e) =>
      isStr(e.school) && isStr(e.degree)
        ? [{ school: e.school, degree: e.degree, place: str(e.place), period: str(e.period), coursework: strs(e.coursework) }]
        : [],
    ),
    experience: rows(raw.experience).flatMap((e) =>
      isStr(e.role) && isStr(e.org)
        ? [{ role: e.role, org: e.org, place: str(e.place), period: str(e.period), bullets: strs(e.bullets) }]
        : [],
    ),
    projects: rows(raw.projects).flatMap((p) =>
      isStr(p.name) ? [{ name: p.name, stack: str(p.stack), status: str(p.status), bullets: strs(p.bullets) }] : [],
    ),
    skills: rows(raw.skills).flatMap((s) => (isStr(s.group) ? [{ group: s.group, items: strs(s.items) }] : [])),
    leadership: rows(raw.leadership).flatMap((l) =>
      isStr(l.role) && isStr(l.org) ? [{ role: l.role, org: l.org, period: str(l.period), bullets: strs(l.bullets) }] : [],
    ),
    achievements: strs(raw.achievements),
    certifications: strs(raw.certifications),
    memberships: strs(raw.memberships),
    sources: rows(raw.sources).flatMap((s) => (isStr(s.label) && isStr(s.path) ? [{ label: s.label, path: s.path }] : [])),
  };
}

export function profilePath(): string {
  return process.env.SCOUT_PROFILE_FILE ?? path.join(process.cwd(), ".local", "profile.json");
}

export async function loadProfile(): Promise<Profile | null> {
  try {
    // turbopackIgnore: profilePath() is dynamic (env override or a
    // gitignored local file), and without this, Turbopack traces the
    // whole project into the output trying to pin it down statically.
    return parse(JSON.parse(await readFile(/* turbopackIgnore: true */ profilePath(), "utf8")));
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------
// Portfolio snapshot — the same personal-data rules apply. Produced by
// scripts/sync-portfolio.mjs from the portfolio site's own content file.

export type Block = { p: string } | { h: string } | { li: string[] } | { links: { label: string; href: string }[] };

export type Portfolio = {
  syncedAt?: string;
  person: { name?: string; role?: string; tagline?: string; status?: string; email?: string };
  links: { label: string; href: string; display?: string }[];
  projects: {
    id: string;
    name: string;
    summary: string;
    why: string;
    detail: string;
    status: string;
    stack: string[];
    context?: string;
    repo?: string;
    featured: boolean;
    demoFile?: string;
    demoNote?: string;
  }[];
  docs: { id: string; blocks: Block[] }[];
};

function parseBlock(b: Record<string, unknown>): Block[] {
  if (isStr(b.p)) return [{ p: b.p }];
  if (isStr(b.h)) return [{ h: b.h }];
  if (Array.isArray(b.li)) return [{ li: strs(b.li) }];
  if (Array.isArray(b.links)) {
    const links = list(b.links, isRecord).flatMap((l) =>
      isStr(l.label) && isStr(l.href) ? [{ label: l.label, href: l.href }] : [],
    );
    return [{ links }];
  }
  return [];
}

function parsePortfolio(raw: unknown): Portfolio | null {
  if (!isRecord(raw) || !isRecord(raw.person)) return null;
  const rows = (v: unknown) => list(v, isRecord);
  const person = raw.person;
  return {
    syncedAt: str(raw.syncedAt),
    person: {
      name: str(person.name),
      role: str(person.role),
      tagline: str(person.tagline),
      status: str(person.status),
      email: str(person.email),
    },
    links: rows(raw.links).flatMap((l) =>
      isStr(l.label) && isStr(l.href) ? [{ label: l.label, href: l.href, display: str(l.display) }] : [],
    ),
    projects: rows(raw.projects).flatMap((p) =>
      isStr(p.id) && isStr(p.name)
        ? [
            {
              id: p.id,
              name: p.name,
              summary: str(p.summary) ?? "",
              why: str(p.why) ?? "",
              detail: str(p.detail) ?? "",
              status: str(p.status) ?? "",
              stack: strs(p.stack),
              context: str(p.context),
              repo: str(p.repo),
              featured: p.featured === true,
              demoFile: str(p.demoFile),
              demoNote: str(p.demoNote),
            },
          ]
        : [],
    ),
    docs: rows(raw.docs).flatMap((d) =>
      isStr(d.id) ? [{ id: d.id, blocks: list(d.blocks, isRecord).flatMap(parseBlock) }] : [],
    ),
  };
}

export function portfolioPath(): string {
  return process.env.SCOUT_PORTFOLIO_SNAPSHOT ?? path.join(process.cwd(), ".local", "portfolio.json");
}

export async function loadPortfolio(): Promise<Portfolio | null> {
  try {
    // turbopackIgnore: same reasoning as loadProfile above.
    return parsePortfolio(JSON.parse(await readFile(/* turbopackIgnore: true */ portfolioPath(), "utf8")));
  } catch {
    return null;
  }
}

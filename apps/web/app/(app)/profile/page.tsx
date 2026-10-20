import { CopyButton } from "@/components/CopyButton";
import { PageHeader } from "@/components/ui";
import { loadPortfolio, loadProfile, profilePath, type Block, type Portfolio, type Profile } from "@/lib/profile";

// A read-only reference sheet of the user's own details — contact, summary,
// experience, projects, skills — so any of it can be copied into an
// application form. Data comes from a gitignored local file (lib/profile.ts),
// so this page is empty on a deployment that doesn't have the file.
export const dynamic = "force-dynamic";

function Section({ title, children, copy }: { title: string; children: React.ReactNode; copy?: string }) {
  return (
    <section className="border border-border-subtle bg-bg-surface mb-6">
      <div className="flex items-center justify-between gap-3 px-4 py-2.5 border-b border-border-subtle bg-text-primary text-bg-base">
        <h2 className="text-micro">{title}</h2>
        {copy && <CopyButton text={copy} label="Copy all" onDark />}
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}

function Bullets({ items }: { items: string[] }) {
  return (
    <ul className="space-y-2 mt-2">
      {items.map((b) => (
        <li key={b} className="flex gap-3 items-start">
          <span aria-hidden className="mt-2 w-1.5 h-1.5 bg-text-primary shrink-0" />
          <span className="text-body text-text-secondary flex-1 min-w-0">{b}</span>
          <CopyButton text={b} />
        </li>
      ))}
    </ul>
  );
}

function Entry({ title, sub, meta, bullets }: { title: string; sub?: string; meta?: string; bullets: string[] }) {
  return (
    <div className="py-4 border-b border-border-subtle last:border-b-0 first:pt-0 last:pb-0">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h3 className="text-heading text-text-primary">{title}</h3>
        {meta && <span className="text-micro text-text-muted">{meta}</span>}
      </div>
      {sub && <p className="text-caption text-text-muted mt-0.5">{sub}</p>}
      {bullets.length > 0 && <Bullets items={bullets} />}
    </div>
  );
}


function Blocks({ blocks }: { blocks: Block[] }) {
  return (
    <div className="space-y-3">
      {blocks.map((b, i) => {
        if ("h" in b) return <h3 key={i} className="text-heading text-text-primary pt-2 first:pt-0">{b.h}</h3>;
        if ("p" in b) return <p key={i} className="text-body text-text-secondary max-w-3xl">{b.p}</p>;
        if ("li" in b) return <Bullets key={i} items={b.li} />;
        return (
          <ul key={i} className="space-y-1">
            {b.links.map((l) => (
              <li key={l.href}>
                <a href={l.href} className="text-body text-text-primary underline underline-offset-2">{l.label}</a>
              </li>
            ))}
          </ul>
        );
      })}
    </div>
  );
}

function PortfolioBody({ pf }: { pf: Portfolio }) {
  const doc = (id: string) => pf.docs.find((d) => d.id === id)?.blocks;
  const about = doc("about");
  const now = doc("now");
  const notes = doc("notes");
  return (
    <>
      {pf.links.length > 0 && (
        <Section title="Links">
          <dl className="divide-y divide-border-subtle -my-2">
            {pf.links.map((l) => (
              <div key={l.href} className="flex items-center gap-4 py-2">
                <dt className="text-micro text-text-muted w-36 shrink-0">{l.label}</dt>
                <dd className="text-body flex-1 min-w-0 break-words font-mono">
                  <a href={l.href} className="underline underline-offset-2">{l.display ?? l.href}</a>
                </dd>
                <CopyButton text={l.href} />
              </div>
            ))}
          </dl>
        </Section>
      )}

      {(pf.person.tagline || pf.person.status) && (
        <Section title="How I describe myself">
          <dl className="space-y-3">
            {pf.person.role && <Row k="Role" v={pf.person.role} />}
            {pf.person.tagline && <Row k="Tagline" v={pf.person.tagline} />}
            {pf.person.status && <Row k="Looking for" v={pf.person.status} />}
          </dl>
        </Section>
      )}

      {about && <Section title="About me"><Blocks blocks={about} /></Section>}

      {pf.projects.length > 0 && (
        <Section title={`Projects (${pf.projects.length})`}>
          {pf.projects.map((p) => (
            <div key={p.id} className="py-4 border-b border-border-subtle last:border-b-0 first:pt-0 last:pb-0">
              <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
                <h3 className="text-heading text-text-primary">
                  {p.name}
                  {p.featured && <span className="text-micro text-text-muted ml-2">featured</span>}
                </h3>
                <span className="text-micro text-text-muted">{p.status === "in-progress" ? "In progress" : "Shipped"}</span>
              </div>
              <p className="text-body text-text-primary mt-1">{p.summary}</p>
              <p className="text-body text-text-secondary mt-2 max-w-3xl">{p.detail}</p>
              {p.why && <p className="text-caption text-text-muted mt-2 max-w-3xl">Why: {p.why}</p>}
              {p.context && <p className="text-caption text-text-muted mt-1">{p.context}</p>}
              <div className="flex flex-wrap items-center gap-1.5 mt-3">
                {p.stack.map((t) => (
                  <span key={t} className="text-caption border border-border-subtle px-2 py-0.5 text-text-secondary">{t}</span>
                ))}
                {p.repo && (
                  <a href={p.repo} className="text-micro border border-border-strong px-2 py-0.5 ml-auto hover:bg-accent-subtle">
                    Repo
                  </a>
                )}
              </div>
              {p.demoFile && (
                <p className="text-caption text-text-muted mt-2">
                  Demo: <span className="font-mono text-text-secondary">{p.demoFile}</span>
                  {p.demoNote ? ` — ${p.demoNote}` : ""}
                </p>
              )}
            </div>
          ))}
        </Section>
      )}

      {now && <Section title="Now"><Blocks blocks={now} /></Section>}
      {notes && <Section title="Field notes, receipts and talking points"><Blocks blocks={notes} /></Section>}
    </>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex items-start gap-4">
      <dt className="text-micro text-text-muted w-36 shrink-0 pt-0.5">{k}</dt>
      <dd className="text-body text-text-primary flex-1 min-w-0">{v}</dd>
      <CopyButton text={v} />
    </div>
  );
}

function Body({ p }: { p: Profile }) {
  return (
    <>
      <Section title="Contact">
        <dl className="divide-y divide-border-subtle -my-2">
          {p.contact.map((c) => (
            <div key={c.label + c.value} className="flex items-center gap-4 py-2">
              <dt className="text-micro text-text-muted w-36 shrink-0">{c.label}</dt>
              <dd className="text-body text-text-primary flex-1 min-w-0 break-words font-mono">{c.value}</dd>
              <CopyButton text={c.value} />
            </div>
          ))}
        </dl>
      </Section>

      {p.summary && (
        <Section title="Summary" copy={p.summary}>
          <p className="text-body text-text-secondary max-w-3xl">{p.summary}</p>
        </Section>
      )}

      {p.education.length > 0 && (
        <Section title="Education">
          {p.education.map((e) => (
            <div key={e.school}>
              <div className="flex flex-wrap items-baseline justify-between gap-x-4">
                <h3 className="text-heading text-text-primary">{e.school}</h3>
                {e.period && <span className="text-micro text-text-muted">{e.period}</span>}
              </div>
              <p className="text-body text-text-secondary mt-0.5">{e.degree}</p>
              {e.place && <p className="text-caption text-text-muted">{e.place}</p>}
              {e.coursework && e.coursework.length > 0 && (
                <p className="text-caption text-text-muted mt-2">Coursework: {e.coursework.join(", ")}</p>
              )}
            </div>
          ))}
        </Section>
      )}

      {p.experience.length > 0 && (
        <Section title="Experience">
          {p.experience.map((e) => (
            <Entry key={e.role + e.org} title={`${e.role}, ${e.org}`} sub={e.place} meta={e.period} bullets={e.bullets} />
          ))}
        </Section>
      )}

      {p.projects.length > 0 && (
        <Section title="Projects (resume wording)">
          {p.projects.map((x) => (
            <Entry
              key={x.name}
              title={x.name}
              sub={x.stack}
              meta={x.status}
              bullets={x.bullets}
            />
          ))}
        </Section>
      )}

      {p.skills.length > 0 && (
        <Section title="Skills" copy={p.skills.map((s) => `${s.group}: ${s.items.join(", ")}`).join("\n")}>
          <dl className="divide-y divide-border-subtle -my-3">
            {p.skills.map((s) => (
              <div key={s.group} className="py-3 sm:flex gap-4">
                <dt className="text-micro text-text-muted sm:w-48 shrink-0 mb-1 sm:mb-0">{s.group}</dt>
                <dd className="flex flex-wrap gap-1.5 flex-1 min-w-0">
                  {s.items.map((i) => (
                    <span key={i} className="text-caption border border-border-subtle px-2 py-0.5 text-text-secondary">
                      {i}
                    </span>
                  ))}
                </dd>
              </div>
            ))}
          </dl>
        </Section>
      )}

      {p.leadership.length > 0 && (
        <Section title="Leadership">
          {p.leadership.map((l) => (
            <Entry key={l.role + l.org} title={`${l.role}, ${l.org}`} meta={l.period} bullets={l.bullets} />
          ))}
        </Section>
      )}

      {(p.achievements.length > 0 || p.certifications.length > 0 || p.memberships.length > 0) && (
        <Section title="Achievements, certifications, memberships">
          <div className="grid gap-6 sm:grid-cols-3">
            {(
              [
                ["Achievements", p.achievements],
                ["Certifications", p.certifications],
                ["Memberships", p.memberships],
              ] as const
            ).map(([label, items]) =>
              items.length > 0 ? (
                <div key={label}>
                  <h3 className="text-micro text-text-muted mb-2">{label}</h3>
                  <ul className="space-y-1.5">
                    {items.map((i) => (
                      <li key={i} className="text-body text-text-secondary">
                        {i}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null,
            )}
          </div>
        </Section>
      )}

    </>
  );
}

function Sources({ p }: { p: Profile }) {
  if (p.sources.length === 0) return null;
  return (
    <Section title="Where this came from">
      <ul className="space-y-1.5">
        {p.sources.map((s) => (
          <li key={s.path} className="text-caption text-text-muted">
            {s.label}: <span className="font-mono text-text-secondary">{s.path}</span>
          </li>
        ))}
      </ul>
    </Section>
  );
}

export default async function ProfilePage() {
  const [profile, portfolio] = await Promise.all([loadProfile(), loadPortfolio()]);

  return (
    <div className="max-w-4xl mx-auto page">
      <PageHeader
        title="Profile"
        subtitle={
          profile
            ? [profile.name, profile.headline, profile.location].filter(Boolean).join(" · ")
            : "Your details, in one place, ready to copy into applications."
        }
      />
      {profile ? (
        <>
          <Body p={profile} />
          {portfolio && <PortfolioBody pf={portfolio} />}
          <Sources p={profile} />
        </>
      ) : (
        <div className="border border-border-subtle bg-bg-surface p-6">
          <h2 className="text-heading text-text-primary">No profile file found</h2>
          <p className="text-body text-text-secondary mt-2 max-w-xl">
            This page reads a local JSON file that is never committed. Put it at{" "}
            <span className="font-mono">{profilePath()}</span> or set <span className="font-mono">SCOUT_PROFILE_FILE</span>.
          </p>
        </div>
      )}
    </div>
  );
}

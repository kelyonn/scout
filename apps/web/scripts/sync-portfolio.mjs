// Snapshots the portfolio site's content (its src/data/site.ts) into
// apps/web/.local/portfolio.json so the Profile page can show it.
// Re-run after editing the portfolio:  node scripts/sync-portfolio.mjs
//
// The output is personal data and lives in the gitignored .local/ folder.
// The source path defaults to ~/Documents/projects/system32/src/data/site.ts
// and can be overridden with SCOUT_PORTFOLIO_FILE.
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";
import vm from "node:vm";
import ts from "typescript";

const src = process.env.SCOUT_PORTFOLIO_FILE ?? path.join(homedir(), "Documents/projects/system32/src/data/site.ts");
const out = path.join(process.cwd(), ".local", "portfolio.json");

const js = ts.transpileModule(readFileSync(src, "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

const sandbox = { exports: {}, module: { exports: {} } };
vm.runInNewContext(js, sandbox);
const site = sandbox.exports.site;
if (!site || !site.person) throw new Error(`no \`site\` export found in ${src}`);

const docs = Object.fromEntries(site.docs.map((d) => [d.id, d.blocks]));
const snapshot = {
  source: src,
  syncedAt: new Date().toISOString(),
  person: site.person,
  links: site.links,
  projects: site.projects.map((p) => ({
    id: p.id,
    name: p.name,
    summary: p.summary,
    why: p.why,
    detail: p.detail,
    status: p.status,
    stack: p.stack,
    context: p.context ?? null,
    repo: p.repo ?? null,
    featured: Boolean(p.featured),
    demoFile: p.demo ? p.demo.src : null,
    demoNote: p.demo?.note ?? null,
  })),
  docs: ["about", "experience", "now", "stack", "notes"].flatMap((id) => (docs[id] ? [{ id, blocks: docs[id] }] : [])),
};

mkdirSync(path.dirname(out), { recursive: true });
writeFileSync(out, JSON.stringify(snapshot, null, 2) + "\n");
console.log(`wrote ${path.relative(process.cwd(), out)}: ${snapshot.projects.length} projects, ${snapshot.docs.length} documents`);

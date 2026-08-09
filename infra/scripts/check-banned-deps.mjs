#!/usr/bin/env node
// Compliance gate: fail the build if a banned dependency appears in any
// dependency manifest.
//
// This enforces AGENTS.md rule 1a and docs/14-legal-compliance.md. Unofficial
// WhatsApp libraries automate WhatsApp Web or reimplement its protocol; Meta
// enforces by banning the phone number, which is the user's real account with
// every recruiter conversation in it. WhatsApp is not a Scout channel at all
// (ADR-013), so nothing should pull these in today.
//
// The check exists because a prohibition that lives only in a document gets
// violated by whoever is in a hurry six months from now. It is a manifest scan
// rather than a lint rule so it catches a transitive dependency too.

// globSync landed in node:fs in Node 22; package.json pins engines.node >= 22.
import { readFileSync, globSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('../..', import.meta.url).pathname;

/** Packages that must never appear, in any manifest, at any depth. */
const BANNED = [
  'whatsapp-web.js',
  'baileys',
  '@whiskeysockets/baileys',
  '@adiwajshing/baileys',
  'venom-bot',
  'wa-automate',
  '@open-wa/wa-automate',
  'whatsapp-web-js',
  'wppconnect',
  '@wppconnect-team/wppconnect',
  'node-whatsapp',
  'yowsup',
];

const MANIFEST_GLOBS = [
  'package.json',
  '**/package.json',
  'pnpm-lock.yaml',
  'go.mod',
  '**/go.mod',
  'go.sum',
  'pyproject.toml',
  '**/pyproject.toml',
  'uv.lock',
  'requirements*.txt',
  '**/requirements*.txt',
  'apps/mobile/android/app/build.gradle',
  'apps/mobile/android/app/build.gradle.kts',
];

function findManifests() {
  const seen = new Set();
  for (const pattern of MANIFEST_GLOBS) {
    let matches = [];
    try {
      matches = globSync(pattern, {
        cwd: ROOT,
        exclude: (p) => p.includes('node_modules') || p.startsWith('.git/'),
      });
    } catch {
      continue;
    }
    for (const m of matches) seen.add(m);
  }
  return [...seen].sort();
}

const violations = [];
const manifests = findManifests();

for (const rel of manifests) {
  let content;
  try {
    content = readFileSync(join(ROOT, rel), 'utf8');
  } catch {
    continue;
  }

  content.split('\n').forEach((line, i) => {
    // Skip comments so a doc-comment naming a banned package (like this file,
    // or the AGENTS.md rule itself) does not trip the gate.
    const trimmed = line.trim();
    if (
      trimmed.startsWith('//') ||
      trimmed.startsWith('#') ||
      trimmed.startsWith('*') ||
      trimmed.startsWith('"comment"')
    ) {
      return;
    }

    for (const pkg of BANNED) {
      if (line.includes(pkg)) {
        violations.push({ file: rel, line: i + 1, pkg, text: trimmed });
      }
    }
  });
}

if (violations.length > 0) {
  console.error('\nCOMPLIANCE FAILURE — banned dependency detected\n');
  for (const v of violations) {
    console.error(`  ${v.file}:${v.line}  ${v.pkg}`);
    console.error(`    ${v.text}\n`);
  }
  console.error(
    'Unofficial WhatsApp libraries violate WhatsApp\'s terms, and Meta enforces\n' +
      'by banning the phone number — the real account, with every recruiter\n' +
      'conversation in it. WhatsApp is not a Scout channel (ADR-013).\n\n' +
      'If outbound messaging is genuinely needed, it is the official Cloud API\n' +
      'or nothing. See AGENTS.md rule 1a and docs/14-legal-compliance.md.\n'
  );
  process.exit(1);
}

console.log(
  `compliance: ${manifests.length} manifest(s) scanned, ` +
    `${BANNED.length} banned packages, 0 violations`
);

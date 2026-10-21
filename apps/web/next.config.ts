import type { NextConfig } from "next";

// Defense-in-depth response headers. Scout has no public ingress
// (ADR-014) and no cross-site flows, so the strict settings cost nothing:
// nothing legitimately frames this app, no third party is embedded, and
// the Go API is the only thing it talks to. A full Content-Security-Policy
// is deliberately not set here — Next.js's own inline runtime scripts
// need a per-request nonce to coexist with one, and a CSP that has to be
// loosened to 'unsafe-inline' to work is mostly theater. Revisit with
// nonce-based CSP if the app ever gains a public origin.
const securityHeaders = [
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), payment=(), usb=()" },
  { key: "X-DNS-Prefetch-Control", value: "off" },
];

const nextConfig: NextConfig = {
  // Traces the minimal set of files each route actually needs and copies
  // them (plus a small server.js) into .next/standalone — infra/docker/
  // web.Dockerfile's runtime stage ships only that, not node_modules or
  // the pnpm store, which is most of why the Go services' images are a
  // few MB and a naive Next.js image would otherwise be hundreds.
  //
  // Skipped on Vercel (which sets VERCEL=1 on every build): Vercel does
  // its own file tracing and serverless packaging, and the two conflict —
  // "standalone" output there produces a build that completes but then
  // fails Vercel's own onBuildComplete step with ENOENT on
  // .next/next-server.js.nft.json, since Vercel never looks for the
  // .next/standalone directory this mode produces.
  output: process.env.VERCEL ? undefined : "standalone",
  // The token never reaches the browser, so nothing here needs to know
  // it — and the `X-Powered-By: Next.js` banner is free information.
  poweredByHeader: false,
  // Hides Next's floating dev-mode badge (bottom-left). It never ships in
  // production builds; this only quiets `next dev`, including on the phone.
  devIndicators: false,
  async headers() {
    return [{ source: "/(.*)", headers: securityHeaders }];
  },
};

export default nextConfig;

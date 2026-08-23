import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Traces the minimal set of files each route actually needs and copies
  // them (plus a small server.js) into .next/standalone — infra/docker/
  // web.Dockerfile's runtime stage ships only that, not node_modules or
  // the pnpm store, which is most of why the Go services' images are a
  // few MB and a naive Next.js image would otherwise be hundreds.
  output: "standalone",
};

export default nextConfig;

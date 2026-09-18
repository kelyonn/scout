import type { MetadataRoute } from "next";

// docs/12-frontend-ux.md section 7's manifest — the installable-shell
// subset. No service worker yet (offline reading and background-synced
// mutations are a separate, larger piece of work); this just makes
// "Add to Home Screen" produce a real standalone window with the right
// name, colors, and shortcuts.
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Scout",
    short_name: "Scout",
    description: "Internship and new-grad software roles, found and ranked for you.",
    start_url: "/",
    display: "standalone",
    background_color: "#282828",
    theme_color: "#282828",
    icons: [{ src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "any" }],
    shortcuts: [
      { name: "Opportunities", url: "/opportunities" },
      { name: "Saved", url: "/saved" },
      { name: "Search", url: "/search" },
    ],
  };
}

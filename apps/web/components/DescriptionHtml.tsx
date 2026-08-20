"use client";

import { useMemo } from "react";
import DOMPurify from "dompurify";

// docs/07-normalization-taxonomy.md section 11's storage-time allowlist,
// applied here as the *render*-time half of the same "defense in depth"
// rule that section states: "sanitized again client-side, and rendered
// through a component that cannot execute script. Defense in depth,
// because a bug in the storage sanitizer should not become stored XSS."
// Storage-time sanitization (before description_html is written to the
// database) is not implemented yet — the Greenhouse over-escaping fix
// (adapters/ats/greenhouse's html.UnescapeString) restored real tag
// structure, but nothing strips a hostile tag out of it before storage.
// This component is a complete defense on its own regardless: DOMPurify
// runs before anything reaches the DOM, so a script tag or an
// event-handler attribute from a compromised or malicious career page
// never executes here even though it's still sitting in the database
// today. Tightening storage is real follow-up work, not a shortcut this
// component depends on for its own safety.
const ALLOWED_TAGS = [
  "p", "br", "ul", "ol", "li", "strong", "em", "b", "i",
  "h1", "h2", "h3", "h4", "h5", "h6", "a", "code", "pre",
  "blockquote", "table", "thead", "tbody", "tr", "td", "th",
];

export function DescriptionHtml({ html }: { html: string }) {
  const clean = useMemo(() => {
    // DOMPurify needs a real DOM; this component only ever mounts once
    // job data has arrived from useQuery (client-side fetch, never
    // populated during SSR), but the guard costs nothing and avoids a
    // crash if that ever changes.
    if (typeof window === "undefined") return "";

    // ALLOWED_ATTR only controls which attributes may appear, not their
    // value — docs/07 section 11 also requires forcing
    // rel="noopener noreferrer nofollow" on every link, not just
    // permitting a caller-supplied rel. Hooks are global to the module's
    // singleton instance, so re-adding the same named behavior on every
    // render is harmless (DOMPurify dedupes by function identity) but
    // removing it first keeps this component idempotent regardless.
    DOMPurify.removeHook("afterSanitizeAttributes");
    DOMPurify.addHook("afterSanitizeAttributes", (node) => {
      if (node.tagName === "A") {
        node.setAttribute("rel", "noopener noreferrer nofollow");
        node.setAttribute("target", "_blank");
      }
    });

    return DOMPurify.sanitize(html, {
      ALLOWED_TAGS,
      ALLOWED_ATTR: ["href"],
      ALLOWED_URI_REGEXP: /^https?:\/\//i,
    });
  }, [html]);

  return (
    <div
      className="prose-description text-body text-text-secondary"
      // Safe: `clean` is DOMPurify output, an allowlist of formatting-only
      // tags with href the only permitted attribute, itself restricted to
      // http(s). No script, no event handlers, no arbitrary attributes
      // reach the DOM.
      dangerouslySetInnerHTML={{ __html: clean }}
    />
  );
}

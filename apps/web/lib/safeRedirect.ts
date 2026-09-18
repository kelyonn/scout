// Login's `?next=` parameter is attacker-controllable (anyone can send a
// link to /login?next=…), so it is only ever honored as a same-origin
// path. A protocol-relative "//evil.example" or an absolute URL would
// otherwise be handed straight to router.replace after a successful
// sign-in — an open redirect off the back of the one credential prompt
// in the product.
export function safeNextPath(raw: string | null | undefined, fallback = "/"): string {
  if (!raw) return fallback;
  // Must be an absolute path on this origin: exactly one leading slash,
  // no backslashes (browsers normalize "\" to "/", turning "/\evil.com"
  // into "//evil.com"), and no embedded CR/LF.
  if (!raw.startsWith("/") || raw.startsWith("//") || /[\\\r\n]/.test(raw)) return fallback;
  // Never bounce back into the login page itself.
  if (raw === "/login" || raw.startsWith("/login?") || raw.startsWith("/login/")) return fallback;
  return raw;
}

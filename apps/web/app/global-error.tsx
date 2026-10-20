"use client";

// Last-resort boundary: catches errors thrown by the root layout itself
// (which app/error.tsx can't, since it renders inside that layout). Has
// to render its own <html>/<body> and can't rely on globals.css having
// loaded, hence the inline styles.
export default function GlobalError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <html lang="en">
      <body
        style={{
          margin: 0,
          minHeight: "100vh",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "#eeede8",
          color: "#0a0a0a",
          fontFamily: "-apple-system, 'Segoe UI', sans-serif",
        }}
      >
        <div style={{ textAlign: "center", padding: 32 }}>
          <h1 style={{ fontSize: 20, fontWeight: 900, margin: 0, textTransform: "uppercase" }}>Scout couldn&rsquo;t load</h1>
          <p style={{ color: "#5b5b57", fontSize: 14, marginTop: 8 }}>Something failed before the page could render.</p>
          <button
            type="button"
            onClick={reset}
            style={{
              marginTop: 20,
              background: "#0a0a0a",
              color: "#fff",
              border: 0,
              borderRadius: 0,
              padding: "10px 16px",
              fontSize: 13,
              fontWeight: 500,
              cursor: "pointer",
            }}
          >
            Try again
          </button>
        </div>
      </body>
    </html>
  );
}

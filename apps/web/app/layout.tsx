import type { Metadata, Viewport } from "next";
import { Archivo, Space_Mono } from "next/font/google";
import "./globals.css";
import { Providers } from "./providers";

const archivo = Archivo({
  variable: "--font-archivo",
  subsets: ["latin"],
  axes: ["wdth"],
});

const spaceMono = Space_Mono({
  variable: "--font-space-mono",
  subsets: ["latin"],
  weight: ["400", "700"],
});

export const metadata: Metadata = {
  title: { default: "Scout", template: "%s · Scout" },
  description: "Internship and new-grad software roles, found and ranked for you.",
  applicationName: "Scout",
  manifest: "/manifest.webmanifest",
  appleWebApp: { capable: true, title: "Scout", statusBarStyle: "black-translucent" },
};

export const viewport: Viewport = {
  themeColor: "#eeede8",
  colorScheme: "light",
  // viewport-fit=cover is what makes env(safe-area-inset-*) non-zero on
  // phones with a notch or home indicator — the bottom tab bar pads by
  // it (components/Shell.tsx).
  viewportFit: "cover",
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${archivo.variable} ${spaceMono.variable} h-full antialiased`}>
      <body className="min-h-full flex flex-col">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}

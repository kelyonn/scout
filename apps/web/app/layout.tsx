import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";
import { Providers } from "./providers";

const inter = Inter({
  variable: "--font-inter",
  subsets: ["latin"],
});

const jetbrainsMono = JetBrains_Mono({
  variable: "--font-jetbrains-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: { default: "Scout", template: "%s · Scout" },
  description: "Internship and new-grad software roles, found and ranked for you.",
  applicationName: "Scout",
  manifest: "/manifest.webmanifest",
  appleWebApp: { capable: true, title: "Scout", statusBarStyle: "black-translucent" },
};

export const viewport: Viewport = {
  themeColor: "#282828",
  colorScheme: "dark",
  // viewport-fit=cover is what makes env(safe-area-inset-*) non-zero on
  // phones with a notch or home indicator — the bottom tab bar pads by
  // it (components/Shell.tsx).
  viewportFit: "cover",
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${inter.variable} ${jetbrainsMono.variable} h-full antialiased`}>
      <body className="min-h-full flex flex-col">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}

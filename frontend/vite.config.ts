import { defineConfig, type Plugin } from 'vite';

// KaTeX ships each font in woff2, woff and ttf. MDv embeds frontend/dist into
// the binary (see the //go:embed in main.go) and only ever renders in
// WKWebView, which has supported woff2 for a decade — so the woff and ttf
// fallbacks are ~875 KB of binary that can never be fetched. Drop those
// candidates from the @font-face src lists and Vite stops emitting the files.
function katexWoff2Only(): Plugin {
  return {
    name: 'katex-woff2-only',
    enforce: 'pre',
    transform(code, id) {
      if (!id.includes('katex') || !id.includes('.css')) return null;
      const trimmed = code.replace(/src:([^};]+)/g, (whole, list: string) => {
        const woff2 = list
          .split(',')
          .map((candidate) => candidate.trim())
          .filter((candidate) => candidate.includes('.woff2'));
        return woff2.length > 0 ? `src:${woff2.join(',')}` : whole;
      });
      return { code: trimmed, map: null };
    },
  };
}

export default defineConfig({
  plugins: [katexWoff2Only()],
});

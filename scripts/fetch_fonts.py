#!/usr/bin/env python3
"""
Regenerate the self-hosted web font set for gostash.

Fetches variable/static woff2 files from Google Fonts (latin + latin-ext
subsets) and writes a local @font-face stylesheet (fonts.css) that the
Go binary embeds via go:embed. Run from the fonts/ directory:

    python3 scripts/fetch_fonts.py

Fonts are licensed under the SIL Open Font License 1.1 (see OFL.txt).
"""
import re, subprocess, os, pathlib

UA = ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
      "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")

# (key, css2 family spec, local filename prefix)
# Use weight ranges (..) for variable fonts; explicit instances for static ones.
FONTS = [
    # --- Serifs (the stars for long-form reading) ---
    ("merriweather", "Merriweather:wght@400..700",                 "merriweather"),
    ("sourceserif",  "Source+Serif+4:opsz,wght@8..60,400..700",    "sourceserif"),
    ("literata",     "Literata:opsz,wght@7..72,400..700",          "literata"),
    ("newsreader",   "Newsreader:opsz,wght@6..72,400..700",        "newsreader"),
    ("spectral",     "Spectral:wght@400;600;700",               "spectral"),
    ("crimsonpro",   "Crimson+Pro:wght@400..700",                  "crimsonpro"),
    ("ebgaramond",   "EB+Garamond:wght@400..700",               "ebgaramond"),
    ("lora",         "Lora:wght@400..700",                         "lora"),
    # --- Sans (sought-after) ---
    ("opensans",     "Open+Sans:wght@400..700",                    "opensans"),
    ("inter",        "Inter:wght@400..700",                        "inter"),
    ("ibmplexsans",  "IBM+Plex+Sans:wght@400..700",              "ibmplexsans"),
    ("firasans",     "Fira+Sans:wght@400;600;700",               "firasans"),
]

KEEP = {"latin", "latin-ext"}

def fetch(url):
    return subprocess.run(["curl", "-s", "-H", f"User-Agent: {UA}", url],
                          capture_output=True, text=True).stdout

def main():
    out_dir = pathlib.Path(__file__).resolve().parent.parent / "internal" / "server" / "static" / "fonts"
    out_dir.mkdir(parents=True, exist_ok=True)

    # clear previous woff2 + css
    for f in out_dir.glob("*.woff2"):
        f.unlink()
    css_path = out_dir / "fonts.css"

    css_out, info = [], []
    for key, spec, prefix in FONTS:
        css = fetch(f"https://fonts.googleapis.com/css2?family={spec}&display=swap")
        if not css.strip():
            print(f"  !! {key}: empty response (bad spec?) — {spec}")
            continue
        blocks = re.findall(r'/\*\s*([\w-]+)\s*\*/\s*(@font-face\s*\{[^}]+\})', css)
        seen_urls = set()
        for subset, face in blocks:
            if subset not in KEEP:
                continue
            urlm = re.search(r'src:\s*url\((https://[^)]+\.woff2)\)', face)
            if not urlm:
                continue
            url = urlm.group(1)
            if url in seen_urls:           # dedupe identical variable-font URLs
                continue
            seen_urls.add(url)
            wm = re.search(r'font-weight:\s*([0-9.]+\s*(?:..)?\s*[0-9.]*)', face)
            wtoken = (wm.group(1).strip() if wm else "x").replace(" ", "").replace("..", "-")
            fname = f"{prefix}-{subset}-{wtoken}.woff2"
            subprocess.run(["curl", "-s", "-H", f"User-Agent: {UA}", "-o",
                            str(out_dir / fname), url], check=True)
            size = os.path.getsize(out_dir / fname)
            local = face.replace(url, f"/static/fonts/{fname}")
            css_out.append(f"/* {key} {subset} {wtoken} */\n{local}")
            info.append((fname, size))
        if not any(i[0].startswith(prefix) for i in info):
            print(f"  !! {key}: no latin subsets matched — {spec}")

    css_path.write_text("\n\n".join(css_out) + "\n")
    total = sum(s for _, s in info)
    print(f"\nDownloaded {len(info)} files ({len(FONTS)} fonts), {total/1024:.0f} KB total")
    for f, s in sorted(info):
        print(f"  {f:38s} {s:>8} bytes")

if __name__ == "__main__":
    main()
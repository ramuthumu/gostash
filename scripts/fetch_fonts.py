import re, subprocess, os, pathlib

UA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

# (key, css family spec, local prefix). Use weight range for variable fonts.
fonts = [
    ("merriweather", "Merriweather:wght@400..700",            "merriweather"),
    ("sourceserif",  "Source+Serif+4:opsz,wght@8..60,400..700","sourceserif"),
    ("literata",     "Literata:opsz,wght@7..72,400..700",      "literata"),
    ("opensans",     "Open+Sans:wght@400..700",                "opensans"),
    ("inter",        "Inter:wght@400..700",                    "inter"),
]
KEEP = {"latin", "latin-ext"}

def fetch(url):
    return subprocess.run(["curl","-s","-H",f"User-Agent: {UA}", url], capture_output=True, text=True).stdout

css_out, info = [], []
for key, spec, prefix in fonts:
    css = fetch(f"https://fonts.googleapis.com/css2?family={spec}&display=swap")
    blocks = re.findall(r'/\*\s*([\w-]+)\s*\*/\s*(@font-face\s*\{[^}]+\})', css)
    for subset, face in blocks:
        if subset not in KEEP: continue
        urlm = re.search(r'src:\s*url\((https://[^)]+\.woff2)\)', face)
        if not urlm: continue
        url = urlm.group(1)
        fname = f"{prefix}-{subset}.woff2"
        subprocess.run(["curl","-s","-H",f"User-Agent: {UA}", "-o", fname, url], check=True)
        size = os.path.getsize(fname)
        local = face.replace(url, f"/static/fonts/{fname}")
        css_out.append(f"/* {key} {subset} */\n{local}")
        info.append((fname, size))

pathlib.Path("fonts.css").write_text("\n\n".join(css_out) + "\n")
for f,s in info: print(f"  {f:32s} {s:>8} bytes")
print(f"Total: {sum(s for _,s in info)} bytes across {len(info)} files")

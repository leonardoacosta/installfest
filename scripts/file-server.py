#!/usr/bin/env python3
"""File server for cmux embedded browser.

Serves local files over HTTP, renders markdown as styled HTML.
Designed to work with OSC 8 terminal hyperlinks + cmux URL routing.

Usage:
    file-server.py [--port PORT] [--bind ADDR]
"""

import hmac
import http.server
import html
import json
import mimetypes
import os
import secrets
import sys
from http.cookies import SimpleCookie
from pathlib import Path
from urllib.parse import parse_qs, unquote, urlsplit

PORT = int(os.environ.get("FILE_SERVER_PORT", 8787))
BIND = os.environ.get("FILE_SERVER_BIND", "0.0.0.0")

ALLOWED_ROOTS = [
    Path.home() / "dev",
]

TOKEN_FILE = Path(
    os.environ.get("FILE_SERVER_TOKEN_FILE", Path.home() / ".local/state/file-server.token")
)


def load_or_create_token() -> str:
    """Read the shared auth token, generating one (0600) on first run."""
    if TOKEN_FILE.exists():
        return TOKEN_FILE.read_text().strip()
    token = secrets.token_hex(16)
    TOKEN_FILE.parent.mkdir(parents=True, exist_ok=True)
    # Create with 0600 from the start — no world-readable window (secret file).
    fd = os.open(TOKEN_FILE, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write(token)
    print(f"file-server: generated new auth token at {TOKEN_FILE}", file=sys.stderr)
    return token


TOKEN = load_or_create_token()

DARK_STYLE = """
  :root { color-scheme: dark; }
  body {
    max-width: 860px; margin: 40px auto; padding: 0 24px;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    line-height: 1.7; color: #c9d1d9; background: #0d1117;
  }
  h1, h2, h3, h4 { color: #e6edf3; margin-top: 1.5em; }
  a { color: #58a6ff; }
  pre { background: #161b22; padding: 16px; border-radius: 8px; overflow-x: auto; }
  code { background: #161b22; padding: 2px 6px; border-radius: 4px; font-size: 0.9em; }
  pre code { background: none; padding: 0; }
  table { border-collapse: collapse; width: 100%; margin: 1em 0; }
  th, td { border: 1px solid #30363d; padding: 8px 12px; text-align: left; }
  th { background: #161b22; color: #e6edf3; }
  tr:nth-child(even) { background: #161b2208; }
  blockquote { border-left: 3px solid #30363d; margin: 1em 0; padding-left: 16px; color: #8b949e; }
  img { max-width: 100%; border-radius: 8px; }
  hr { border: none; border-top: 1px solid #30363d; margin: 2em 0; }
  .breadcrumb { font-size: 0.85em; color: #8b949e; margin-bottom: 1em; }
  .breadcrumb a { color: #58a6ff; text-decoration: none; }
"""

MD_TEMPLATE = """<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<script src="https://cdn.jsdelivr.net/npm/marked@14/marked.min.js"></script>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/highlight.js@11/styles/github-dark.min.css">
<script src="https://cdn.jsdelivr.net/npm/highlight.js@11/highlight.min.js"></script>
<style>{style}</style>
</head><body>
<div class="breadcrumb">{breadcrumb}</div>
<div id="content"></div>
<script>
marked.setOptions({{
  highlight: function(code, lang) {{
    if (lang && hljs.getLanguage(lang)) {{
      return hljs.highlight(code, {{ language: lang }}).value;
    }}
    return hljs.highlightAuto(code).value;
  }}
}});
// Disable raw-HTML passthrough (matches ropen-server's markdown-it({{html:false}})) —
// untrusted markdown must never execute an embedded <script>/<tag>.
marked.use({{
  renderer: {{
    html(token) {{
      const raw = typeof token === 'string' ? token : ((token && (token.raw || token.text)) || '');
      return raw.replace(/</g, '&lt;');
    }}
  }}
}});
document.getElementById('content').innerHTML = marked.parse({content_json});
</script>
</body></html>"""

HTML_WRAPPER = """<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
</head><body>
{content}
</body></html>"""

PLAIN_TEMPLATE = """<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<style>{style}
  body {{ font-family: 'SF Mono', 'Fira Code', monospace; }}
  pre {{ white-space: pre-wrap; word-wrap: break-word; }}
</style>
</head><body>
<div class="breadcrumb">{breadcrumb}</div>
<pre>{content}</pre>
</body></html>"""


def breadcrumb(path: Path) -> str:
    """Build a clickable breadcrumb trail."""
    parts = path.parts
    crumbs = []
    for i, part in enumerate(parts):
        if i == len(parts) - 1:
            crumbs.append(f"<strong>{html.escape(part)}</strong>")
        else:
            crumbs.append(html.escape(part))
    return " / ".join(crumbs)


def is_allowed(path: Path) -> bool:
    """Check path is under an allowed root."""
    resolved = path.resolve()
    return any(resolved.is_relative_to(root.resolve()) for root in ALLOWED_ROOTS)


def _is_trusted_html(path: Path) -> bool:
    """True only for self-generated recon/report HTML (docs/recon/*.html) —
    the narrow, deliberately-named trust boundary for serving a full <html>
    document live. Being under an ALLOWED_ROOTS entry (~/dev) is NOT
    sufficient — that directory holds arbitrary cloned third-party repos
    whose HTML must never be trusted just because it's reachable."""
    try:
        resolved = path.resolve()
    except OSError:
        return False
    return resolved.parent.name == "recon" and resolved.parent.parent.name == "docs"


class FileHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        parts = urlsplit(self.path)

        supplied = parse_qs(parts.query).get("t", [None])[0]
        via_param = supplied is not None and hmac.compare_digest(supplied, TOKEN)
        via_cookie = False
        cookie_header = self.headers.get("Cookie")
        if cookie_header:
            morsel = SimpleCookie(cookie_header).get("fs_token")
            if morsel is not None:
                via_cookie = hmac.compare_digest(morsel.value, TOKEN)

        if not (via_param or via_cookie):
            self.send_error(403, "Missing or invalid token")
            return

        # On a fresh param match, set a cookie so relative links (dir listings,
        # rendered markdown) work without threading ?t= through every href.
        self._auth_cookie = TOKEN if via_param and not via_cookie else None

        raw_path = unquote(parts.path.lstrip("/"))
        if not raw_path:
            self.send_error(400, "No file path specified")
            return

        path = Path("/" + raw_path)

        if not is_allowed(path):
            self.send_error(403, f"Not in allowed directories: {path}")
            return

        if not path.exists():
            self.send_error(404, f"Not found: {path}")
            return

        if path.is_dir():
            self._serve_directory(path)
            return

        suffix = path.suffix.lower()

        if suffix in (".md", ".markdown", ".mdx"):
            self._serve_markdown(path)
        elif suffix in (".html", ".htm"):
            self._serve_html(path)
        elif suffix in (".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico"):
            self._serve_binary(path)
        elif suffix == ".pdf":
            self._serve_binary(path)
        else:
            self._serve_plain(path)

    def _serve_markdown(self, path: Path):
        content = path.read_text(errors="replace")
        body = MD_TEMPLATE.format(
            title=html.escape(path.name),
            style=DARK_STYLE,
            breadcrumb=breadcrumb(path),
            content_json=json.dumps(content),
        )
        self._respond(200, "text/html", body.encode())

    def _serve_html(self, path: Path):
        content = path.read_text(errors="replace")
        # If it's a full HTML doc, serve as-is ONLY when it's self-generated
        # recon/report output we trust (docs/recon/*.html); anything else
        # full-HTML is untrusted — serve inert as text/plain rather than let
        # the browser execute it. NOTE: "under ~/dev" alone is NOT a trust
        # boundary here — ALLOWED_ROOTS already confines every servable path
        # to ~/dev (harden-mesh-file-servers task 1.6), which itself holds
        # numerous cloned third-party repos; a broader "is it under ~/dev"
        # check would make this branch always-true and serve arbitrary HTML
        # from those repos live.
        if "<html" in content.lower()[:500]:
            if _is_trusted_html(path):
                self._respond(200, "text/html", content.encode())
            else:
                self._respond(200, "text/plain", content.encode())
        else:
            body = HTML_WRAPPER.format(
                title=html.escape(path.name),
                content=content,
            )
            self._respond(200, "text/html", body.encode())

    def _serve_plain(self, path: Path):
        content = path.read_text(errors="replace")
        body = PLAIN_TEMPLATE.format(
            title=html.escape(path.name),
            style=DARK_STYLE,
            breadcrumb=breadcrumb(path),
            content=html.escape(content),
        )
        self._respond(200, "text/html", body.encode())

    def _serve_binary(self, path: Path):
        mime = mimetypes.guess_type(str(path))[0] or "application/octet-stream"
        self._respond(200, mime, path.read_bytes())

    def _serve_directory(self, path: Path):
        """Simple directory listing."""
        entries = sorted(path.iterdir(), key=lambda p: (not p.is_dir(), p.name.lower()))
        items = []
        for entry in entries:
            name = entry.name + ("/" if entry.is_dir() else "")
            href = f"/{entry}"
            items.append(f'<li><a href="{html.escape(href)}">{html.escape(name)}</a></li>')

        body = f"""<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>{html.escape(str(path))}</title>
<style>{DARK_STYLE} li {{ margin: 4px 0; }}</style>
</head><body>
<div class="breadcrumb">{breadcrumb(path)}</div>
<ul>{"".join(items)}</ul>
</body></html>"""
        self._respond(200, "text/html", body.encode())

    def _respond(self, code: int, content_type: str, body: bytes):
        self.send_response(code)
        self.send_header("Content-Type", f"{content_type}; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-cache")
        if getattr(self, "_auth_cookie", None):
            self.send_header("Set-Cookie", f"fs_token={self._auth_cookie}; Path=/; HttpOnly")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        # Quiet by default; set FILE_SERVER_VERBOSE=1 to enable
        if os.environ.get("FILE_SERVER_VERBOSE"):
            super().log_message(format, *args)


def main():
    if any(a in ("-h", "--help") for a in sys.argv[1:]):
        print(__doc__)
        return

    port = PORT
    bind = BIND

    for i, arg in enumerate(sys.argv[1:], 1):
        if arg == "--port" and i < len(sys.argv) - 1:
            port = int(sys.argv[i + 1])
        elif arg == "--bind" and i < len(sys.argv) - 1:
            bind = sys.argv[i + 1]

    server = http.server.HTTPServer((bind, port), FileHandler)
    print(f"file-server listening on {bind}:{port}")
    print(f"Serving: {', '.join(str(r) for r in ALLOWED_ROOTS)}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down.")
        server.shutdown()


if __name__ == "__main__":
    main()

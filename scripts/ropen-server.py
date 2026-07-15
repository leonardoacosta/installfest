#!/usr/bin/env python3
"""ropen-server — multi-mount HTTP file server with client-side markdown rendering.

Used by ~/.local/bin/ropen (CLI) for both:
  - Background mode: spawned by `ropen <file>` if no server is already running
  - Foreground mode: invoked by `ropen --serve` for systemd-user supervision

Reads:
  - mounts state at $1 (path to mounts.json under /tmp/ropen-<uid>/)
  - registered projects from this repo's home/projects.toml (read on every
    render — DOTFILES env var if set, else ~/dev/personal/installfest)

Args:
  sys.argv[1] = port (int)
  sys.argv[2] = mounts.json path (str)
"""
import sys, os, time, pathlib, mimetypes, json
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from urllib.parse import unquote

port = int(sys.argv[1])
mounts_path = sys.argv[2]

DOTFILES = pathlib.Path(os.environ.get('DOTFILES', str(pathlib.Path.home() / 'dev' / 'personal' / 'installfest')))
REGISTRY_PATH = DOTFILES / 'home' / 'projects.toml'

def load_registry():
    """Load registered projects from home/projects.toml. Returns list of
    {code, name, path}. Uses project codes as mount slugs for the index.
    Filters non-existent paths. Fails open (empty list) on any parse error —
    a broken/missing registry must never take down file serving."""
    try:
        import tomllib
        with open(REGISTRY_PATH, 'rb') as f:
            data = tomllib.load(f)
    except Exception:
        return []
    out = []
    for p in data.get('projects', []) or []:
        code = p.get('code')
        raw_path = p.get('path', '')
        if not code or not raw_path:
            continue
        try:
            resolved = (pathlib.Path.home() / raw_path).expanduser().resolve()
        except Exception:
            continue
        if not resolved.is_dir():
            continue
        out.append({
            'code': code,
            'name': p.get('name', code),
            'path': resolved,
        })
    return out

def load_state():
    try:
        with open(mounts_path) as f:
            data = json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return {}, {}, load_registry()
    raw_mounts = data.get("mounts", {}) or {}
    mounts = {}
    for k, v in raw_mounts.items():
        try:
            mounts[k] = pathlib.Path(v).resolve()
        except Exception:
            pass
    sentinels = data.get("sentinels", {}) or {}
    return mounts, sentinels, load_registry()

MD_TEMPLATE = """\
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>##TITLE##</title>
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/github-markdown-css/5.5.1/github-markdown-dark.min.css">
<style>
  :root { --bg: #0d1117; --surface: #161b22; --border: #30363d; }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html { background: var(--bg); }
  body { max-width: none; margin: 0; padding: 2rem 2.5rem; background: var(--bg); color: #e6edf3; }
  .markdown-body { background: var(--bg) !important; font-size: 15px; line-height: 1.7; }
  .markdown-body table { display: table; width: 100%; }
  .markdown-body table th, .markdown-body table td { padding: 8px 12px; border: 1px solid var(--border); font-size: 14px; }
  .markdown-body table th { background: var(--surface); font-weight: 600; }
  .markdown-body table tr:nth-child(2n) { background: rgba(255,255,255,0.02); }
  .markdown-body h1 { border-bottom: 1px solid var(--border); padding-bottom: 0.3em; margin-bottom: 1em; }
  .markdown-body h2 { border-bottom: 1px solid var(--border); padding-bottom: 0.2em; margin: 1.5em 0 0.6em; }
  .markdown-body blockquote { border-left: 3px solid #3fb950; background: rgba(63,185,80,0.05); padding: 0.5em 1em; }
  .markdown-body code { background: var(--surface); padding: 0.2em 0.4em; border-radius: 4px; font-size: 13px; }
  .markdown-body pre { background: var(--surface) !important; border: 1px solid var(--border); border-radius: 6px; padding: 1em; overflow-x: auto; }
  .markdown-body pre code { background: none; padding: 0; }
  .markdown-body strong { color: #f0f6fc; }
  .markdown-body a { color: #58a6ff; }
  #render-error { background: #5a1d1d; border: 1px solid #f85149; padding: 1em; border-radius: 6px; color: #ffa198; font-family: monospace; font-size: 13px; white-space: pre-wrap; }
  #live-indicator {
    position: fixed; top: 12px; right: 12px;
    display: flex; align-items: center; gap: 6px;
    font: 11px/1 -apple-system, sans-serif;
    color: #7d8590; opacity: 0.7; transition: opacity 0.3s;
  }
  #live-indicator:hover { opacity: 1; }
  #live-indicator .mount { color: #58a6ff; font-weight: 600; }
  #live-dot { width: 6px; height: 6px; border-radius: 50%; background: #3fb950; animation: pulse 2s infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.4; } }
  #live-indicator.disconnected #live-dot { background: #f85149; animation: none; }
</style>
</head>
<body class="markdown-body">
<div id="content"><p style="color:#7d8590">Rendering ##TITLE##…</p></div>
<div id="live-indicator"><span id="live-dot"></span>live<span class="mount">##MOUNT##</span></div>
<script src="https://cdn.jsdelivr.net/npm/markdown-it@14/dist/markdown-it.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
<script>
  // Client-side render: fetch raw markdown via ?raw=1, render via markdown-it,
  // post-process mermaid code blocks. Eliminates a Python markdown dep so
  // ropen survives system resets that strip pip/pacman state.
  //
  // Defense in depth: if markdown-it CDN didn't load (offline, firewall, etc.),
  // fall back to <pre>-formatted raw markdown so the user always sees content.
  const mount = '##MOUNT##';
  const rawUrl = window.location.pathname + '?raw=1';
  const content = document.getElementById('content');

  function showError(msg) {
    const div = document.createElement('div');
    div.id = 'render-error';
    div.textContent = 'ropen render failed:\\n' + msg;
    content.replaceChildren(div);
  }

  function showFallback(src, reason) {
    // Plain-text fallback when markdown-it isn't available
    const banner = document.createElement('div');
    banner.id = 'render-error';
    banner.textContent = 'ropen: ' + reason + ' — showing raw markdown';
    const pre = document.createElement('pre');
    pre.style.background = 'var(--surface)';
    pre.style.padding = '1em';
    pre.style.whiteSpace = 'pre-wrap';
    pre.textContent = src;
    content.replaceChildren(banner, pre);
  }

  async function render() {
    let src;
    try {
      const res = await fetch(rawUrl, { cache: 'no-store' });
      if (!res.ok) throw new Error('fetch ' + rawUrl + ' returned HTTP ' + res.status);
      src = await res.text();
    } catch (err) {
      showError((err && (err.stack || err.message)) || String(err));
      return;
    }

    if (typeof window.markdownit !== 'function') {
      showFallback(src, 'markdown-it CDN unavailable (window.markdownit undefined)');
      return;
    }

    try {
      const md = window.markdownit({ html: false, linkify: true, typographer: true, breaks: false });
      content.innerHTML = md.render(src);
    } catch (err) {
      showFallback(src, 'markdown-it render error: ' + ((err && err.message) || err));
      return;
    }

    // Promote mermaid fences to <div class="mermaid"> (best-effort; no-op if mermaid CDN failed)
    try {
      content.querySelectorAll('pre > code').forEach(function(el) {
        const txt = el.textContent.trim();
        const isMermaidLang = el.className.indexOf('language-mermaid') !== -1;
        const looksMermaid = /^(graph|sequenceDiagram|classDiagram|flowchart|gantt|pie|erDiagram|journey|gitGraph|mindmap|timeline)\\b/.test(txt);
        if (isMermaidLang || looksMermaid) {
          const div = document.createElement('div');
          div.className = 'mermaid';
          div.textContent = txt;
          el.parentElement.replaceWith(div);
        }
      });
      if (typeof window.mermaid !== 'undefined' && typeof window.mermaid.initialize === 'function') {
        window.mermaid.initialize({ startOnLoad: false, theme: 'dark' });
        window.mermaid.run();
      }
    } catch (err) {
      console.error('ropen: mermaid post-process failed (non-fatal):', err);
    }
  }
  render();

  const ind = document.getElementById('live-indicator');
  function connect() {
    try {
      const es = new EventSource('/__sse/' + mount);
      es.onmessage = function() { location.reload(); };
      es.onerror = function() {
        ind.classList.add('disconnected');
        es.close();
        setTimeout(connect, 2000);
      };
      es.onopen = function() { ind.classList.remove('disconnected'); };
    } catch (err) {
      console.error('ropen: SSE connect failed (non-fatal):', err);
      ind.classList.add('disconnected');
    }
  }
  connect();
</script>
</body></html>"""

def render_md_shell(path, mount):
    """Emit HTML wrapper that fetches the raw .md and renders client-side."""
    return (MD_TEMPLATE
            .replace('##TITLE##', path.name)
            .replace('##MOUNT##', mount)
           ).encode('utf-8')

INDEX_HTML = """\
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>ropen mounts</title>
<style>
  body { font: 14px/1.5 -apple-system, system-ui, sans-serif; background: #0d1117; color: #e6edf3; max-width: 760px; margin: 4rem auto; padding: 2rem; }
  h1 { font-size: 20px; font-weight: 600; margin: 0 0 1.5rem; color: #f0f6fc; }
  ul { list-style: none; padding: 0; }
  li { margin: 0.5rem 0; padding: 0.75rem 1rem; background: #161b22; border: 1px solid #30363d; border-radius: 6px; }
  a { color: #58a6ff; text-decoration: none; font-weight: 600; }
  a:hover { text-decoration: underline; }
  .path { color: #7d8590; font-size: 12px; margin-top: 0.25rem; font-family: ui-monospace, monospace; }
  .empty { color: #7d8590; font-style: italic; }
</style>
</head>
<body>
##LIST##
</body></html>"""

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        mounts, sentinels, registry = load_state()

        if self.path == '/__mounts':
            payload = json.dumps({"mounts": {k: str(v) for k, v in mounts.items()}}, indent=2).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return

        # SSE live reload: /__sse/<mount>
        if self.path.startswith('/__sse/'):
            mount = unquote(self.path[len('/__sse/'):])
            sentinel_path = sentinels.get(mount)
            if not sentinel_path or not os.path.exists(sentinel_path):
                self.send_error(404, f'Unknown mount: {mount}'); return
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Cache-Control', 'no-cache')
            self.send_header('Connection', 'keep-alive')
            self.send_header('Access-Control-Allow-Origin', '*')
            self.end_headers()
            last_mtime = os.path.getmtime(sentinel_path)
            try:
                while True:
                    time.sleep(0.5)
                    try:
                        cur = os.path.getmtime(sentinel_path)
                    except FileNotFoundError:
                        break
                    if cur != last_mtime:
                        last_mtime = cur
                        self.wfile.write(b'data: reload\n\n')
                        self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError, OSError):
                pass
            return

        # Root: render index with active mounts + registered projects
        req = unquote(self.path.split('?', 1)[0]).lstrip('/')
        if not req:
            sections = []
            if mounts:
                items = '<ul>' + ''.join(
                    f'<li><a href="/{k}/">{k}</a><div class="path">{v}</div></li>'
                    for k, v in sorted(mounts.items())
                ) + '</ul>'
                sections.append('<h1>Active mounts</h1>' + items)

            # Registered projects (from projects.toml) — code-as-slug, path lazy-resolved
            active_paths = {str(v) for v in mounts.values()}
            registry_unique = [r for r in registry if str(r['path']) not in active_paths]
            if registry_unique:
                items = '<ul>' + ''.join(
                    f'<li><a href="/{r["code"]}/">{r["code"]}</a> <span style="color:#7d8590">{r["name"]}</span><div class="path">{r["path"]}</div></li>'
                    for r in sorted(registry_unique, key=lambda r: r['code'])
                ) + '</ul>'
                heading = '<h1>Registered projects</h1>' if not mounts else '<h2 style="margin-top:2rem;color:#f0f6fc;font-size:18px;font-weight:600">Registered projects</h2>'
                sections.append(heading + items)

            if not sections:
                sections.append('<h1>ropen — active mounts</h1><p class="empty">No mounts or registered projects. Run <code>ropen &lt;file&gt;</code> to add one.</p>')

            body = INDEX_HTML.replace('##LIST##', ''.join(sections)).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        # Parse: /<mount>/<rest>
        segments = req.split('/', 1)
        mount = segments[0]
        rest = segments[1] if len(segments) > 1 else ''

        # Resolve serve_dir: active mount → registered project → backwards-compat → 404
        serve_dir = None
        if mount in mounts:
            serve_dir = mounts[mount]
        else:
            # Try registered projects (code-as-slug)
            reg_match = next((r for r in registry if r['code'] == mount), None)
            if reg_match:
                serve_dir = reg_match['path']
            elif len(mounts) == 1:
                # Backwards-compat fallback: tabs opened before v4 upgrade
                only_mount = next(iter(mounts))
                mount = only_mount
                rest = req
                serve_dir = mounts[mount]
            else:
                known = sorted(set(list(mounts.keys()) + [r['code'] for r in registry]))
                self.send_error(404, f'Unknown mount: {mount}. Known: {", ".join(known) or "(none)"}')
                return

        try:
            target = (serve_dir / rest).resolve() if rest else serve_dir
            target.relative_to(serve_dir)  # path-traversal guard
        except (ValueError, OSError):
            self.send_error(403, 'Forbidden')
            return

        if target.is_dir():
            for idx in ('index.md', 'README.md', 'index.html'):
                if (target / idx).exists():
                    target = target / idx
                    break
            else:
                # No index file — render directory listing instead of 404
                try:
                    children = sorted(target.iterdir(), key=lambda p: (not p.is_dir(), p.name.lower()))
                except OSError as e:
                    self.send_error(500, f'Cannot list directory: {e}')
                    return
                rel = target.relative_to(serve_dir)
                rel_str = str(rel) if str(rel) != '.' else ''
                items = []
                if rel_str:
                    parent_url = '../'
                    items.append(f'<li><a href="{parent_url}">../</a></li>')
                for child in children:
                    if child.name.startswith('.'):
                        continue
                    suffix = '/' if child.is_dir() else ''
                    items.append(f'<li><a href="{child.name}{suffix}">{child.name}{suffix}</a></li>')
                title = f'{mount}/{rel_str}' if rel_str else mount
                listing = f'<h1 style="font-family:ui-monospace,monospace;font-size:16px">{title}</h1><ul>' + ''.join(items) + '</ul>'
                body = INDEX_HTML.replace('##LIST##', listing).encode()
                self.send_response(200)
                self.send_header('Content-Type', 'text/html; charset=utf-8')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return

        if not target.is_file():
            self.send_error(404, 'File not found')
            return

        if target.suffix.lower() == '.md':
            # ?raw=1 → return the raw .md content (used by the client-side
            # markdown-it renderer in MD_TEMPLATE). Otherwise → return the
            # wrapper HTML page that fetches ?raw=1 and renders in-browser.
            raw_mode = '?raw=1' in self.path or '&raw=1' in self.path
            try:
                if raw_mode:
                    data = target.read_bytes()
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/markdown; charset=utf-8')
                    self.send_header('Cache-Control', 'no-store')
                    self.send_header('Content-Length', str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
                else:
                    data = render_md_shell(target, mount)
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/html; charset=utf-8')
                    self.send_header('Content-Length', str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
            except Exception as e:
                import traceback
                tb = traceback.format_exc()
                self.send_error(500, f"{e}\n\n{tb}")
        else:
            ctype, _ = mimetypes.guess_type(str(target))
            data = target.read_bytes()
            self.send_response(200)
            self.send_header('Content-Type', ctype or 'application/octet-stream')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    def log_message(self, *args):
        pass

if __name__ == '__main__':
    print(f'ropen-server listening on :{port} (mounts={mounts_path})', file=sys.stderr, flush=True)
    ThreadingHTTPServer(('0.0.0.0', port), Handler).serve_forever()

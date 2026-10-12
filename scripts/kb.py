#!/usr/bin/env python3
"""Knowledge-base tooling for PRD-001 §9.11 (AI-52, AI-53): index, lint, stale, hash and log.

The compile step is done by an LLM (the fh-knowledge-base skill in this repository, the
kb-compiler job at runtime). This script does the deterministic parts: it generates the index
from page front matter, and it lints pages against kb/schema.md. Standard library only.

    python3 scripts/kb.py index            # regenerate kb/index.md
    python3 scripts/kb.py lint             # exit 1 on any error (make kb-lint, part of make check)
    python3 scripts/kb.py stale            # pages whose sources changed, and sources no page covers
    python3 scripts/kb.py hash <path>      # the "sources" line for a source file
    python3 scripts/kb.py log <op> <text>  # append an entry to kb/log.md
"""
import datetime
import hashlib
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
KB = ROOT / "kb"
PAGES = KB / "pages"
INDEX = KB / "index.md"
LOG = KB / "log.md"
SCHEMA = "kb-schema@1"

WATCHED_DIRS = ["docs/user-guide"]           # raw sources (kb/schema.md "Layers")
WATCHED_EXCLUDE = {"docs/user-guide/README.md"}
FORBIDDEN_SOURCES = ("docs/prd/",)            # internal spec: never a source

TYPES = ["concept", "task", "reference", "policy", "decision"]
REQUIRED = ["key", "type", "title", "summary", "sources", "data_class", "taint", "status",
            "human_edited", "compiled_by", "schema", "compiled_at"]
DATA_CLASSES = {"general", "personal", "payroll", "equity", "identity"}
LOG_OPS = {"compile", "edit", "lint", "rollback"}
LOG_RE = re.compile(r"^## \[(\d{4}-\d{2}-\d{2})\] ([a-z]+) \| \S.*$")

CITE_RE = re.compile(r"\(src: ([^)#\s]+)#([^)\s]+)\)")
WIKILINK_RE = re.compile(r"\[\[([a-z0-9.\-]+)\]\]")
ID_RE = re.compile(r"\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-\d+[a-z]?\b|\bP\d\b|\b[A-Z]{2,}\d+\b")
NUM_RE = re.compile(r"(?:S\$|US\$)?\d[\d,]*(?:\.\d+)?(?:%|k\b|m\b)?")


def rel(path):
    return path.relative_to(ROOT).as_posix()


def sha256(relpath):
    return hashlib.sha256((ROOT / relpath).read_bytes()).hexdigest()


def parse_front_matter(text):
    """Parse the restricted front matter of kb/schema.md: `key: value` lines and one list."""
    if not text.startswith("---\n"):
        return None, text
    end = text.find("\n---\n", 4)
    if end < 0:
        return None, text
    fm, current_list = {}, None
    for line in text[4:end].split("\n"):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if line.startswith("  - ") and current_list:
            fm[current_list].append(re.sub(r"\s+#.*$", "", line[4:]).strip())
            continue
        m = re.match(r"^([a-z_]+):\s*(.*)$", line)
        if not m:
            raise ValueError(f"unreadable front-matter line: {line!r}")
        key, value = m.group(1), re.sub(r"\s+#.*$", "", m.group(2)).strip()
        if value == "":
            fm[key], current_list = [], key
        else:
            fm[key], current_list = value.strip('"'), None
    return fm, text[end + 5:]


def slugify(heading):
    """GitHub-style heading anchor."""
    s = heading.strip().lower()
    s = re.sub(r"[^\w\- ]", "", s)
    return s.replace(" ", "-")


def anchors(text):
    seen, out, fenced = {}, set(), False
    for line in text.split("\n"):
        if line.startswith("```"):
            fenced = not fenced
            continue
        m = None if fenced else re.match(r"^#{1,6}\s+(.*?)\s*#*\s*$", line)
        if m:
            slug = slugify(m.group(1))
            n = seen.get(slug, 0)
            seen[slug] = n + 1
            out.add(slug if n == 0 else f"{slug}-{n}")
    return out


def watched_sources():
    found = []
    for d in WATCHED_DIRS:
        for p in sorted((ROOT / d).rglob("*.md")):
            if rel(p) not in WATCHED_EXCLUDE:
                found.append(rel(p))
    return found


def load_pages():
    pages = []
    for p in sorted(PAGES.rglob("*")) if PAGES.exists() else []:
        if p.is_dir():
            continue
        entry = {"path": p, "rel": rel(p), "errors": [], "fm": {}, "body": ""}
        if p.suffix != ".md":
            entry["errors"].append("only Markdown files belong in kb/pages")
        else:
            try:
                fm, body = parse_front_matter(p.read_text(encoding="utf-8"))
                if fm is None:
                    entry["errors"].append("missing front matter")
                else:
                    entry["fm"], entry["body"] = fm, body
            except ValueError as e:
                entry["errors"].append(str(e))
        pages.append(entry)
    return pages


def parse_sources(fm):
    out = []
    for item in fm.get("sources", []) if isinstance(fm.get("sources"), list) else []:
        m = re.match(r"^(\S+)\s+sha256:([0-9a-f]{64})$", item)
        out.append((m.group(1), m.group(2)) if m else (item, None))
    return out


def source_status(relpath):
    fm, _ = parse_front_matter((ROOT / relpath).read_text(encoding="utf-8"))
    return (fm or {}).get("status", "published")


def numbers_outside_ids(body):
    text = re.sub(r"`[^`]*`", " ", body)                 # inline code
    text = CITE_RE.sub(" ", text)                         # citations
    text = WIKILINK_RE.sub(" ", text)                     # page links
    text = re.sub(r"\]\([^)]*\)", "]", text)              # link targets
    text = re.sub(r"(?m)^\s*\d+\.\s", " ", text)          # ordered-list markers
    text = ID_RE.sub(" ", text)                           # requirement and rule IDs
    return [t for t in NUM_RE.findall(text) if any(c.isdigit() for c in t)]


def render_index(pages):
    good = [p for p in pages if p["fm"] and p["fm"].get("key")]
    lines = [
        "# Knowledge-base index",
        "",
        f"Generated by `scripts/kb.py index` from page front matter; do not edit by hand. "
        f"Schema: `{SCHEMA}` ([schema.md](schema.md)). The assistant reads this index first "
        "(PRD-001 AI-51); at runtime it is filtered to what the reader may see (AI-54).",
        "",
        f"{len(good)} pages · {sum(1 for p in good if p['fm'].get('status') == 'draft')} draft",
    ]
    for t in TYPES:
        group = sorted((p for p in good if p["fm"].get("type") == t), key=lambda p: p["fm"]["key"])
        if not group:
            continue
        lines += ["", f"## {t}", "", "| Key | Summary | Status | Data class |", "|---|---|---|---|"]
        for p in group:
            fm = p["fm"]
            link = pathlib.Path(p["rel"]).relative_to("kb").as_posix()
            summary = fm.get("summary", "").replace("|", "\\|")
            lines.append(f"| [{fm['key']}]({link}) | {summary} | {fm.get('status')} | {fm.get('data_class')} |")
    return "\n".join(lines) + "\n"


def lint(pages):
    errors, warnings = [], []
    keys, inbound = {}, {}
    for p in pages:
        if p["fm"].get("key"):
            if p["fm"]["key"] in keys:
                errors.append(f"{p['rel']}: duplicate key {p['fm']['key']} (also {keys[p['fm']['key']]})")
            keys[p["fm"]["key"]] = p["rel"]
    covered = set()
    for p in pages:
        where = p["rel"]
        errors += [f"{where}: {e}" for e in p["errors"]]
        fm, body = p["fm"], p["body"]
        if not fm:
            continue
        for field in REQUIRED:
            if field not in fm or fm[field] in ("", []):
                errors.append(f"{where}: front matter is missing '{field}'")
        if fm.get("type") not in TYPES:
            errors.append(f"{where}: type must be one of {', '.join(TYPES)}")
        elif p["path"].parent.name != fm["type"]:
            errors.append(f"{where}: a '{fm['type']}' page belongs in kb/pages/{fm['type']}/")
        if len(fm.get("summary", "")) > 160:
            errors.append(f"{where}: summary is longer than 160 characters")
        if fm.get("schema") != SCHEMA:
            errors.append(f"{where}: schema must be {SCHEMA}")
        if fm.get("data_class") not in DATA_CLASSES:
            errors.append(f"{where}: data_class must be one of {', '.join(sorted(DATA_CLASSES))}")
        if fm.get("taint") not in ("trusted", "untrusted"):
            errors.append(f"{where}: taint must be trusted or untrusted")
        if fm.get("status") not in ("draft", "published"):
            errors.append(f"{where}: status must be draft or published")
        if fm.get("human_edited") not in ("true", "false"):
            errors.append(f"{where}: human_edited must be true or false")
        if not re.match(r"^\d{4}-\d{2}-\d{2}$", fm.get("compiled_at", "")):
            errors.append(f"{where}: compiled_at must be a date (YYYY-MM-DD)")
        if "aliases" in fm and (not isinstance(fm["aliases"], list) or not fm["aliases"]
                                or any(len(a) > 60 for a in fm["aliases"])):
            errors.append(f"{where}: aliases must be a non-empty list of phrases of at most 60 characters")
        if fm.get("status") == "published" and fm.get("taint") == "untrusted":
            errors.append(f"{where}: an untrusted page stays draft until approved (AI-54)")

        sources, source_text = parse_sources(fm), {}
        for src, digest in sources:
            if digest is None:
                errors.append(f"{where}: source line must be '<path> sha256:<64 hex>': {src}")
                continue
            if src.startswith(FORBIDDEN_SOURCES):
                errors.append(f"{where}: {src} is internal and may not be a source (kb/schema.md)")
            if not (ROOT / src).is_file():
                errors.append(f"{where}: source not found: {src}")
                continue
            covered.add(src)
            source_text[src] = (ROOT / src).read_text(encoding="utf-8")
            if sha256(src) != digest:
                errors.append(f"{where}: STALE, {src} changed since compile; recompile (fh-knowledge-base skill)")
            if fm.get("status") == "published" and source_status(src) == "draft":
                errors.append(f"{where}: published, but its source {src} is a draft")

        if not body.lstrip().startswith("# "):
            warnings.append(f"{where}: the body should start with '# <title>'")
        for section in re.split(r"(?m)^## ", body)[1:]:
            heading = section.split("\n", 1)[0].strip()
            if not CITE_RE.search(section):
                errors.append(f"{where}: section '{heading}' cites no source")
        for src, anchor in CITE_RE.findall(body):
            if src not in source_text:
                errors.append(f"{where}: cites {src}, which is not in its sources")
            elif anchor not in anchors(source_text[src]):
                errors.append(f"{where}: cites {src}#{anchor}, but the source has no such heading")
        for target in WIKILINK_RE.findall(body):
            if target not in keys:
                errors.append(f"{where}: link [[{target}]] matches no page key")
            elif target != fm.get("key"):
                inbound[target] = inbound.get(target, 0) + 1
        corpus = "\n".join(source_text.values())
        for num in sorted(set(numbers_outside_ids(body))):
            if num not in corpus:
                errors.append(f"{where}: the number '{num}' appears in none of its sources (AI-53)")

    for k, where in keys.items():
        if len(keys) > 1 and not inbound.get(k):
            warnings.append(f"{where}: orphan page, no other page links to [[{k}]]")
    for src in watched_sources():
        if src not in covered:
            errors.append(f"{src}: source not compiled into any page")

    expected = render_index(pages)
    if not INDEX.exists() or INDEX.read_text(encoding="utf-8") != expected:
        errors.append("kb/index.md is out of date; run make kb-index")

    if LOG.exists():
        last = ""
        for line in LOG.read_text(encoding="utf-8").split("\n"):
            if line.startswith("## "):
                m = LOG_RE.match(line)
                if not m or m.group(2) not in LOG_OPS:
                    errors.append(f"kb/log.md: entry must be '## [YYYY-MM-DD] <{'|'.join(sorted(LOG_OPS))}> | <text>': {line}")
                elif m.group(1) < last:
                    errors.append(f"kb/log.md: entries out of date order at {line}")
                else:
                    last = m.group(1)
    else:
        errors.append("kb/log.md is missing")
    return errors, warnings


def main(argv):
    if len(argv) < 2 or argv[1] not in {"index", "lint", "stale", "hash", "log"}:
        print(__doc__)
        return 2
    cmd, pages = argv[1], load_pages()
    if cmd == "index":
        INDEX.write_text(render_index(pages), encoding="utf-8")
        print(f"✔ wrote {rel(INDEX)} ({sum(1 for p in pages if p['fm'])} pages)")
        return 0
    if cmd == "hash":
        for path in argv[2:]:
            print(f"  - {path} sha256:{sha256(path)}")
        return 0
    if cmd == "log":
        if len(argv) < 4 or argv[2] not in LOG_OPS:
            print(f"usage: kb.py log <{'|'.join(sorted(LOG_OPS))}> <text>")
            return 2
        with LOG.open("a", encoding="utf-8") as f:
            f.write(f"\n## [{datetime.date.today().isoformat()}] {argv[2]} | {' '.join(argv[3:])}\n")
        return 0
    if cmd == "stale":
        covered = set()
        for p in pages:
            for src, digest in parse_sources(p["fm"]):
                covered.add(src)
                if digest and (ROOT / src).is_file() and sha256(src) != digest:
                    print(f"stale     {p['rel']}  ({src})")
        for src in watched_sources():
            if src not in covered:
                print(f"uncovered {src}")
        return 0
    errors, warnings = lint(pages)
    for w in warnings:
        print(f"  ⚠ {w}")
    for e in errors:
        print(f"  ✖ {e}")
    if errors:
        print(f"{len(errors)} error(s); see kb/schema.md and the fh-knowledge-base skill")
        return 1
    print(f"✔ knowledge base: {len(pages)} pages, {len(warnings)} warning(s), 0 errors")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

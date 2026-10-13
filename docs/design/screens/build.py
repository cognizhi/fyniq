#!/usr/bin/env python3
"""Build the Claude Design canvases for Fyniq's screens (PRD-001 §7.10; fyniq-openspec skill).

Screens are authored as fragments in src/<File>.html (the page content only). This script wraps
each one in the shared app shell, expands Lucide icons, lints it against the canvas format, and
writes one canvas per area: the screen at five device frames (desktop, tablet, foldable unfolded,
phone, foldable folded), in light and dark. Standard library only.

    python3 docs/design/screens/build.py kit                  # kit/fyniq-ui.css and kit/nemura-ui.css (tokens + ui.css [+ nemura.css])
    python3 docs/design/screens/build.py check [File…]        # lint fragments (all by default)
    python3 docs/design/screens/build.py canvas <id> [--ds URL] [--only File,…]
                                                              # out/<id>/project/… ready to publish
    python3 docs/design/screens/build.py ds [nemura]          # out/ds[-nemura]/project/… (design system files)
    python3 docs/design/screens/build.py status               # fragments written per canvas
    python3 docs/design/screens/build.py shots <File> [frame-theme …]  # local PNGs (headless Chrome)
    python3 docs/design/screens/build.py heights <File>… [--write]     # measure content height per class

Fragment format (src/<File>.html):

    <!-- fyniq-screen {"nav": "banking", "title": "Categorise", "mode": "expert",
                       "heights": {"compact": 1500, "medium": 1100, "expanded": 960}} -->
    <div class="fy-page fy-enter"> … </div>
    <!-- @ai -->                      (optional: the rest is the docked AI panel, an <aside class="fy-ai">)

Settings (all optional): layout "app" | "bare"; nav (active navigation key); title (compact top
bar); mode "basic" | "expert"; heights per device class (frames grow to fit, never shrink below
the device); motion "on" | "off"; theme_note (shown on the canvas); for Nemura screens (PRD-002)
persona "employee" | "hr" | "admin" and integrated true | false (shows the Finance | People switch).
A canvas's `product` in inventory.json ("fyniq" by default, or "nemura") picks the tokens, kit,
fonts and shell. Icons: <i data-icon="landmark"
class="fy-i--20"></i> becomes inline Lucide SVG (aria-hidden unless the <i> has aria-label).
"""
import datetime
import html.parser
import json
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[2]
TOKENS = ROOT / "docs/design-system/fyniq/tokens.css"
KIT = HERE / "kit"
SRC = HERE / "src"
OUT = HERE / "out"
INVENTORY = HERE / "inventory.json"
ICONS = json.loads((KIT / "lucide-icon-nodes.json").read_text(encoding="utf-8"))
FONTS = ("https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700"
         "&amp;family=JetBrains+Mono:wght@400;500&amp;display=swap")
FONTS_NEMURA = ("https://fonts.googleapis.com/css2?family=Funnel+Display:wght@600;700;800"
                "&amp;family=Funnel+Sans:wght@400;500;600;700;800&amp;family=Spline+Sans+Mono:wght@400;500&amp;display=swap")

# Products (PRD-001 Fyniq, PRD-002 Nemura): each canvas names one in inventory.json
PRODUCTS = {
    "fyniq": {"name": "Fyniq", "tokens": TOKENS, "kit": "fyniq-ui.css", "extra": None, "fonts": FONTS,
              "ds_dir": "ds", "ds_src": HERE / "ds", "namespace": "fyniq"},
    "nemura": {"name": "Nemura", "tokens": ROOT / "docs/design-system/nemura/tokens.css", "kit": "nemura-ui.css",
               "extra": KIT / "nemura.css", "fonts": FONTS_NEMURA, "ds_dir": "ds-nemura", "ds_src": HERE / "ds-nemura",
               "namespace": "nemura"},
}

# Device frames (CSS px): key, label, width, default height, device class
FRAMES = [
    ("desktop", "Desktop", 1440, 900, "expanded"),
    ("tablet", "Tablet", 834, 1194, "medium"),
    ("fold-open", "Foldable open", 690, 840, "medium"),
    ("phone", "Phone", 390, 844, "compact"),
    ("fold-closed", "Foldable folded", 344, 882, "compact"),
]
THEMES = [("light", "Light"), ("dark", "Dark")]
GAP_X, GAP_ROW, GAP_BLOCK, TITLE_SPACE = 80, 120, 200, 300

# Navigation (PRD §7.1, §7.8): key, label, icon, badge, badge label, expert-only, group
NAV = [
    ("my-claims", "My claims", "receipt-text", "1", "1 claim returned to fix", False, "personal"),
    ("money-in", "Money I put in", "hand-coins", None, None, False, "personal"),
    ("home", "Home", "house", None, None, False, "main"),
    ("inbox", "Inbox", "inbox", "5", "5 items waiting", False, "main"),
    ("sales", "Sales", "receipt", "3", "3 overdue invoices", False, "main"),
    ("purchases", "Purchases", "shopping-cart", "2", "2 bills due", False, "main"),
    ("contacts", "Contacts", "users", None, None, False, "main"),
    ("banking", "Banking", "landmark", "14", "14 lines to categorise", False, "main"),
    ("payroll", "Payroll", "wallet", None, None, False, "main"),
    ("projects", "Projects", "briefcase-business", None, None, False, "main"),
    ("accounting", "Accounting", "book-open", None, None, True, "main"),
    ("reports", "Reports", "chart-column", None, None, False, "main"),
    ("planning", "Planning", "trending-up", None, None, True, "main"),
    ("equity", "Equity", "chart-pie", None, None, False, "main"),
    ("compliance", "Compliance", "shield-check", "1", "1 deadline due", True, "main"),
]
FOOT = [("settings", "Settings", "settings"), ("help", "Help", "life-buoy")]
MODULE_NAV = {"Home": "home", "Inbox": "inbox", "Sales": "sales", "Purchases": "purchases",
              "Contacts": "contacts", "Banking": "banking", "Payroll": "payroll", "Projects": "projects",
              "Accounting": "accounting", "Reports": "reports", "Planning": "planning",
              "Equity": "equity", "Compliance": "compliance", "Settings": "settings", "Help": "help",
              "Personal": "my-claims", "Shell": "home", "Super admin": "settings"}
BOTTOM_FIXED = ["home", "inbox", None, "banking"]


# --- kit -------------------------------------------------------------------------------------

def css_blocks(text):
    """Top-level (selector, body) pairs; @media bodies are returned whole."""
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.S)
    out, i = [], 0
    while True:
        j = text.find("{", i)
        if j < 0:
            return out
        sel = text[i:j].strip()
        depth, k = 1, j + 1
        while depth:
            depth += {"{": 1, "}": -1}.get(text[k], 0)
            k += 1
        out.append((sel, text[j + 1:k - 1]))
        i = k


def decls(body):
    return dict(re.findall(r"(--[\w-]+)\s*:\s*([^;]+);", body)) | (
        {"color-scheme": m.group(1)} if (m := re.search(r"color-scheme:\s*(\w+)", body)) else {})


def themes(tokens=TOKENS):
    blocks = dict(css_blocks(tokens.read_text(encoding="utf-8")))
    root, dark = decls(blocks[":root"]), decls(blocks[".dark"])
    hc, darkhc = decls(blocks['[data-contrast="more"]']), decls(blocks['.dark[data-contrast="more"]'])
    return {
        "light": root,
        "dark": root | dark,
        "light-hc": root | hc,
        "dark-hc": root | dark | hc | darkhc,
    }, blocks


def kit_css(product="fyniq"):
    cfg = PRODUCTS[product]
    th, blocks = themes(cfg["tokens"])
    root = th["light"]
    if product == "fyniq":
        lines = ["/* GENERATED by docs/design/screens/build.py from docs/design-system/fyniq/tokens.css",
                 "   and kit/ui.css. Do not edit: change those files and run `build.py kit`. */", ""]
    else:
        lines = [f"/* GENERATED by docs/design/screens/build.py from {cfg['tokens'].relative_to(ROOT).as_posix()},",
                 f"   kit/ui.css and kit/{cfg['extra'].name}. Do not edit: change those files and run `build.py kit`. */", ""]

    def block(sel, values, base=None):
        body = [f"  {k}: {v};" for k, v in values.items() if base is None or base.get(k) != v]
        lines.extend([f"{sel} {{", *body, "}", ""])

    block(':root, [data-theme="light"]', root)
    for name in ("dark", "light-hc", "dark-hc"):
        block(f'[data-theme="{name}"]', th[name], base=None if name == "dark-hc" else root)
    lines += ['[data-theme="light-hc"] { color-scheme: light; }', '[data-theme="dark-hc"] { color-scheme: dark; }', ""]
    for sel, body in blocks.items():
        if sel.startswith("[data-density") or sel.startswith("[data-motion") or sel.startswith("@media"):
            lines.append(f"{sel} {{{body}}}\n")
    css = "\n".join(lines) + "\n" + (KIT / "ui.css").read_text(encoding="utf-8")
    if cfg["extra"]:
        css += "\n" + cfg["extra"].read_text(encoding="utf-8")
    return css


def cmd_kit(args):
    for product, cfg in PRODUCTS.items():
        css = kit_css(product)
        (KIT / cfg["kit"]).write_text(css, encoding="utf-8")
        print(f"✔ kit/{cfg['kit']} ({len(css) // 1024} KB; themes: light, dark, light-hc, dark-hc)")
    return 0


def canvas_product(canvas_id, inv=None):
    inv = inv or load_inventory()
    c = next((c for c in inv["canvases"] if c["id"] == canvas_id), {})
    return c.get("product", "fyniq")


def file_product(name, inv=None):
    inv = inv or load_inventory()
    s = next((s for s in inv["screens"] if s["file"] == name), None)
    return canvas_product(s["canvas"], inv) if s else "fyniq"


# --- fragments -------------------------------------------------------------------------------

VOID = {"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"}
SETTINGS_RE = re.compile(r"^\s*<!--\s*fyniq-screen\s*(\{.*?\})\s*-->", re.S)
ICON_RE = re.compile(r'<i\s+([^>]*?)data-icon="([a-z0-9-]+)"([^>]*)></i>')
EMOJI_RE = re.compile("[\U0001F000-\U0001FAFF☀-➿⬀-⯿️]")
COLOUR_RE = re.compile(r"#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(|\boklch\(")


def known_classes(product="fyniq"):
    css = (KIT / "ui.css").read_text(encoding="utf-8")
    extra = PRODUCTS[product]["extra"]
    if extra:
        css += extra.read_text(encoding="utf-8")
    return set(re.findall(r"\.((?:fy|nm)-[a-z0-9_-]+)", css))


class Lint(html.parser.HTMLParser):
    def __init__(self, name, classes):
        super().__init__(convert_charrefs=True)
        self.name, self.classes, self.stack, self.errors, self.warnings, self.ids = name, classes, [], [], [], set()

    def err(self, msg):
        self.errors.append(f"{self.name}:{self.getpos()[0]}: {msg}")

    def handle_starttag(self, tag, attrs, closed=False):
        for k, v in attrs:
            if v is None:
                self.err(f"<{tag}> attribute `{k}` has no quoted value (write {k}=\"{k}\" or use aria-*)")
            if k.startswith("on"):
                self.err(f"<{tag}> event attribute `{k}`: static mockups carry no handlers")
            if k == "style" and v and COLOUR_RE.search(v):
                self.err(f"<{tag}> style uses a raw colour; use a token: var(--…)")
            if k == "class" and v:
                for c in v.split():
                    if c.startswith(("fy-", "nm-")) and c not in self.classes:
                        self.warnings.append(f"{self.name}:{self.getpos()[0]}: unknown class `{c}`")
            if k == "id" and v:
                if v in self.ids:
                    self.err(f"duplicate id `{v}`")
                self.ids.add(v)
            if k in ("checked", "selected"):
                self.err(f"<{tag}> `{k}` makes a controlled input; use aria-checked/aria-selected on a button")
        if tag in ("script", "iframe", "object", "embed", "form", "style", "link"):
            self.err(f"<{tag}> is not allowed in a fragment")
        if tag == "img" and not any(k == "alt" for k, _ in attrs):
            self.err("<img> without alt")
        if tag not in VOID and not closed:
            self.stack.append((tag, self.getpos()[0]))

    def handle_startendtag(self, tag, attrs):
        self.handle_starttag(tag, attrs, closed=True)
        if tag not in VOID and tag not in ("path", "circle", "rect", "line", "polyline", "polygon", "ellipse", "stop", "use"):
            self.err(f"<{tag}/> self-closed; write <{tag}></{tag}>")

    def handle_endtag(self, tag):
        if tag in VOID:
            return
        if not self.stack:
            self.err(f"</{tag}> closes nothing")
            return
        open_tag, line = self.stack.pop()
        if open_tag != tag:
            self.err(f"</{tag}> closes <{open_tag}> opened on line {line}")

    def handle_data(self, data):
        if "{{" in data or "}}" in data:
            self.err("`{{`/`}}` in text: the canvas treats it as a template hole")
        if EMOJI_RE.search(data):
            self.err(f"emoji or symbol glyph in text ({EMOJI_RE.search(data).group()!r}); use a Lucide icon and a label")


def read_fragment(path):
    text = path.read_text(encoding="utf-8")
    m = SETTINGS_RE.match(text)
    settings = json.loads(m.group(1)) if m else {}
    body = text[m.end():] if m else text
    main, _, ai = body.partition("<!-- @ai -->")
    return settings, main.strip(), ai.strip()


def lint(path, classes):
    errors, warnings = [], []
    try:
        settings, main, ai = read_fragment(path)
    except json.JSONDecodeError as e:
        return [f"{path.name}: settings comment is not valid JSON: {e}"], []
    for k in settings:
        if k not in ("layout", "nav", "title", "mode", "heights", "motion", "theme_note", "ai", "persona", "integrated", "workspace"):
            warnings.append(f"{path.name}: unknown setting `{k}`")
    for name in ICON_RE.findall(main + ai):
        if name[1] not in ICONS:
            errors.append(f"{path.name}: unknown Lucide icon `{name[1]}`")
    stray = re.findall(r"<i\s[^>]*data-icon=[^>]*>(?!</i>)", main + ai)
    if stray:
        errors.append(f"{path.name}: write icons exactly as <i data-icon=\"name\"></i> ({len(stray)} found)")
    for part in (main, ai):
        p = Lint(path.name, classes)
        p.feed(part)
        p.close()
        errors += p.errors
        warnings += p.warnings
        for tag, line in p.stack:
            errors.append(f"{path.name}:{line}: <{tag}> is never closed")
    return errors, warnings


# --- sample data (AUTHORING.md §6, sample-data.json) -----------------------------------------

SAMPLE = HERE / "sample-data.json"
DOC_RE = re.compile(r"(?<![\w-])(INV|BILL|JNL|CLM|QUO|PO|CN)-((?<=PO-)00\d\d|(?<!PO-)\d{4})\b")
BANKREF_RE = re.compile(r"\b(INV|BILL)0(\d{3})\b")
CODE_RE = re.compile(r"(?<![\w,.\-\u2010-\u2014\u2212/:#$=+@\u2022\u00b7])([1-8]\d{3})(?=[ \u00a0]+[A-Z][a-z&'])")
WEEKDAY_RE = re.compile(r"\b(Mon|Tue|Wed|Thu|Fri|Sat|Sun)[a-z]*,? (\d{1,2}) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|"
                        r"Oct|Nov|Dec)[a-z]*\.?(?: (20\d\d))?")
MONTHS = "Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec".split()
LATER_RE = re.compile(r"(?:\b13 Oct(?: 2026)?,? (?:at )?|\b[Tt]oday,? (?:at )?|\bsaved |\bcalculated |\bTaken )"
                      r"(?P<h>\d{1,2}):(?P<m>\d\d)\b")
PROPOSAL_RE = re.compile(r"(?:[Pp]roposal\s+#?|proposal-|#)(3\d\d)\b(?=[^0-9]{0,40}?(?:[Pp]ending|[Ee]xecuted|[Rr]ejected|[Ee]xpired|[Aa]pproved|bank\.|ar\.|ap\.|gl\.|Categorise|Match|Draft|Accrue|Bank rule))")
MASK_RE = re.compile(r"[STFGM]?\u2022{3,6}(\d{2,4}[A-Z])\b")
TEXT_BLOCKS = {"tr", "li", "p", "h1", "h2", "h3", "dd", "dt", "figcaption", "summary", "label",
               "option", "blockquote", "button", "caption", "legend", "th", "td", "a", "span", "div"}


class Blocks(html.parser.HTMLParser):
    """Collects the text of every element, innermost first, so a check can look at the
    smallest element that holds a document number."""

    VOID = {"br", "img", "input", "meta", "link", "hr", "path", "circle", "rect", "line", "polyline",
            "polygon", "i", "source", "col", "wbr", "use", "ellipse"}

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.stack, self.blocks, self.skip = [], [], 0

    def handle_starttag(self, tag, attrs):
        if tag == "svg":
            self.skip += 1
        if tag in self.VOID or self.skip:
            return
        self.stack.append([tag, []])

    def handle_endtag(self, tag):
        if tag == "svg":
            self.skip = max(0, self.skip - 1)
            return
        if self.skip:
            return
        for i in range(len(self.stack) - 1, -1, -1):
            if self.stack[i][0] == tag:
                parts = []
                while len(self.stack) > i:
                    t, p = self.stack.pop()
                    parts = p + parts
                text = " ".join(parts)
                if tag in TEXT_BLOCKS:
                    self.blocks.append(re.sub(r"\s+", " ", text).strip())
                if self.stack:
                    self.stack[-1][1].append(text)
                return

    def handle_data(self, data):
        if self.stack and not self.skip:
            self.stack[-1][1].append(data)


def plain_text(markup):
    t = re.sub(r"<svg\b.*?</svg>", " ", markup, flags=re.S)
    t = re.sub(r"</(td|th|tr|li|p|a|div|h[1-6]|dd|dt|label|option|button|section|article|header)>",
               " \u00a6 ", t)                     # a cell or block edge ends a code's name
    t = re.sub(r"<[^>]+>", " ", t)
    return re.sub(r"\s+", " ", html.unescape(t))


def check_data(paths):
    """Sample-data consistency: account names, document owners, masked IDs (AUTHORING.md §6)."""
    if not SAMPLE.exists():
        return ["sample-data.json is missing"]
    data = json.loads(SAMPLE.read_text(encoding="utf-8"))
    accounts, docs, people = data["accounts"], data["documents"], data["people"]
    proposals = data.get("proposals", {})
    owner_of = {}
    for full, short in data["contacts"].items():
        for name in [full] + short:
            owner_of[name] = full
    names = sorted(owner_of, key=len, reverse=True)
    contact_re = re.compile("|".join(re.escape(n) for n in names))
    other_names = {n.lower(): c for c, ns in accounts.items() for n in ns}
    errors = []
    for path in paths:
        _, main, ai = read_fragment(path)
        markup = main + "\n" + ai
        text = plain_text(markup)
        # 1. account codes carry their own name
        for m in CODE_RE.finditer(text):
            code, after = m.group(1), text[m.end():].lstrip(" \u00a0")
            if text[:m.start()].rstrip().endswith("\u2022") or contact_re.match(after):
                continue                                   # a masked number, or a sub-ledger line
            if re.match(r"(Dr|Cr|TX|SR|ZR|ES|Box|Q[1-4])\b", after):
                continue                                   # a debit, credit or GST code follows
            if 2020 <= int(code) <= 2035 and code not in accounts:
                continue                                   # a year
            if code in accounts:
                if any(after.lower().startswith(n.lower()) for n in accounts[code]):
                    continue
                clash = next((c for n, c in other_names.items() if after.lower().startswith(n)), None)
                why = f"that name belongs to {clash}" if clash else f"it is {accounts[code][0]!r}"
                errors.append(f"{path.name}: account {code} is followed by {after[:40]!r}; {why}")
            else:
                clash = next((c for n, c in other_names.items() if after.lower().startswith(n)), None)
                if clash:
                    errors.append(f"{path.name}: {code} {after[:30]!r}: that account is {clash}")
                else:
                    errors.append(f"{path.name}: account {code} ({after[:30]!r}) is not in sample-data.json")
        # 2. document numbers belong to one contact
        p = Blocks()
        p.feed(markup)
        p.close()
        seen, checked = set(), {}
        for block in p.blocks:                             # innermost first
            refs = [(f"{t}-{n}", m.start()) for m in DOC_RE.finditer(block) for t, n in [m.groups()]]
            refs += [(f"{t}-0{n}", m.start()) for m in BANKREF_RE.finditer(block) for t, n in [m.groups()]]
            has_contact = bool(contact_re.search(block))
            for ref, at in refs:
                inner = [b for b in checked.get(ref, []) if b in block]
                if block.count(ref) <= sum(b.count(ref) for b in inner):
                    continue                               # a smaller element already settled it
                if has_contact:
                    checked.setdefault(ref, []).append(block)
                doc = docs.get(ref)
                if doc is None:
                    if ref not in seen:
                        errors.append(f"{path.name}: {ref} is not in sample-data.json documents")
                        seen.add(ref)
                    continue
                own = doc.get("contact")
                if not own:
                    continue
                mentioned = {owner_of[c.group(0)] for c in contact_re.finditer(block)}
                near = {owner_of[c.group(0)] for c in contact_re.finditer(block)
                        if abs(c.start() - at) <= 80}
                others = {c for r, _ in refs if docs.get(r) for c in [docs[r].get("contact")] if c}
                stray = near - others
                if own not in mentioned and stray and (ref, block) not in seen:
                    seen.add((ref, block))
                    errors.append(f"{path.name}: {ref} belongs to {own}, but sits next to "
                                  f"{', '.join(sorted(stray))}: {block[:90]!r}")
        # 3. a weekday name matches its date (the year is 2026 unless it is written)
        for m in WEEKDAY_RE.finditer(text):
            day, d, mon, year = m.groups()
            try:
                real = datetime.date(int(year or 2026), MONTHS.index(mon) + 1, int(d)).strftime("%a")
            except ValueError:
                real = "no such date"
            if real != day:
                errors.append(f"{path.name}: {m.group(0)!r} is a {real}")
        # 4. nothing happened after the sample clock (13 Oct 2026, 09:12) unless it is scheduled
        for m in LATER_RE.finditer(text):
            hh, mm = m.group("h"), m.group("m")
            before = text[max(0, m.start() - 24):m.start()]
            if int(hh) * 60 + int(mm) > 9 * 60 + 12 and not re.search(
                    r"(?i)\b(next|expires?|until|stops?|due|retries|scheduled|runs?)\b", before):
                errors.append(f"{path.name}: {m.group(0)!r} is after the sample clock, 13 Oct 09:12")
        # 6. AI proposal numbers are registered
        for m in PROPOSAL_RE.finditer(text):
            if "#" + m.group(1) not in proposals:
                errors.append(f"{path.name}: AI proposal #{m.group(1)} is not in sample-data.json")
        # 5. masked identity numbers match the person
        for block in p.blocks:
            for m in MASK_RE.finditer(block):
                who = [(block.rfind(n, 0, m.start()), n) for n in people if n in block[:m.start()]]
                if not who:
                    continue
                _, person = max(who)
                want = people[person].get("id")
                if want and not want.endswith(m.group(1)) and len(block) < 400:
                    errors.append(f"{path.name}: {person}'s masked ID ends {want}, not {m.group(1)}")
    return sorted(set(errors), key=errors.index)


def check_inventory():
    """Inventory rules: unique ids and files, known canvases, ≤ 17 screens a canvas, real requirement IDs."""
    if not INVENTORY.exists():
        return [], ["inventory.json is missing"]
    inv = load_inventory()
    prd = "\n".join((ROOT / json.loads(src.read_text(encoding="utf-8"))["prd"]).read_text(encoding="utf-8")
                    for src in sorted((ROOT / "openspec/sources").glob("prd-*.json")) if not src.name.endswith(".lock.json"))
    errors, canvases = [], {c["id"] for c in inv["canvases"]}
    for c in inv["canvases"]:
        if c.get("product", "fyniq") not in PRODUCTS:
            errors.append(f"canvas {c['id']}: unknown product {c.get('product')}")
    seen_id, seen_file, per = set(), set(), {}
    for s in inv["screens"]:
        where = f"inventory {s.get('id')}"
        for key in ("id", "file", "title", "canvas", "module", "phase", "requirements"):
            if key not in s:
                errors.append(f"{where}: missing `{key}`")
        if s.get("id") in seen_id:
            errors.append(f"{where}: duplicate id")
        if s.get("file") in seen_file:
            errors.append(f"{where}: duplicate file {s.get('file')}")
        if not re.fullmatch(r"[A-Z][A-Za-z0-9]*", s.get("file", "")):
            errors.append(f"{where}: file must be PascalCase letters and digits")
        if s.get("canvas") not in canvases:
            errors.append(f"{where}: unknown canvas {s.get('canvas')}")
        seen_id.add(s.get("id"))
        seen_file.add(s.get("file"))
        per[s.get("canvas")] = per.get(s.get("canvas"), 0) + 1
        for r in s.get("requirements", []):
            if not re.search(rf"\|\s*\**`?{re.escape(r)}`?\**\s*\|", prd):
                errors.append(f"{where}: requirement {r} is not a row in the PRD")
    for c, n in per.items():
        if n > 17:
            errors.append(f"canvas {c}: {n} screens (at most 17; a canvas loads about 199 files)")
    return errors, []


def cmd_check(args):
    inv = load_inventory()
    classes = {p: known_classes(p) for p in PRODUCTS}
    files = [SRC / f"{a}.html" for a in args] if args else sorted(SRC.glob("*.html"))
    errs = warns = 0
    if not args:
        for product, cfg in PRODUCTS.items():
            kit = KIT / cfg["kit"]
            if not kit.exists() or kit.read_text(encoding="utf-8") != kit_css(product):
                print(f"✖ kit/{cfg['kit']} is out of date: run `build.py kit`")
                errs += 1
        e, w = check_inventory()
        for line in e:
            print(f"✖ {line}")
        for line in w:
            print(f"⚠ {line}")
        errs += len(e)
        warns += len(w)
    for line in check_data(files):
        print(f"✖ {line}")
        errs += 1
    for f in files:
        e, w = lint(f, classes[file_product(f.stem, inv)])
        for line in e:
            print(f"✖ {line}")
        for line in w:
            print(f"⚠ {line}")
        errs += len(e)
        warns += len(w)
    print(f"{'✔' if not errs else '✖'} {len(files)} fragments, {errs} errors, {warns} warnings")
    return 1 if errs else 0


# --- shell -----------------------------------------------------------------------------------

def icon(name, cls="", label=None, style=None):
    nodes = ICONS[name]
    inner = "".join(f"<{t} " + " ".join(f'{k}="{v}"' for k, v in a.items()) + f"></{t}>" for t, a in nodes)
    aria = f'role="img" aria-label="{label}"' if label else 'aria-hidden="true"'
    st = f' style="{style}"' if style else ""
    return f'<svg class="fy-i{(" " + cls) if cls else ""}" viewBox="0 0 24 24" {aria}{st}>{inner}</svg>'


def expand_icons(text):
    def rep(m):
        attrs = m.group(1) + m.group(3)
        cls = re.search(r'class="([^"]*)"', attrs)
        label = re.search(r'aria-label="([^"]*)"', attrs)
        style = re.search(r'style="([^"]*)"', attrs)
        return icon(m.group(2), cls.group(1) if cls else "", label.group(1) if label else None,
                    style.group(1) if style else None)
    return ICON_RE.sub(rep, text)


def shell(screen, settings, main, ai, product="fyniq"):
    if product == "nemura":
        return shell_nemura(screen, settings, main, ai)
    active = settings.get("nav") or MODULE_NAV.get(screen.get("module", ""), "home")
    title = settings.get("title") or screen["title"]
    nav_html, groups = [], {"personal": "For you", "main": "Workspace"}
    for group in ("personal", "main"):
        nav_html.append(f'<div class="fy-nav__group fy-eyebrow">{groups[group]}</div>')
        for key, label, ic, badge, blabel, expert, g in NAV:
            if g != group:
                continue
            cur = ' aria-current="page"' if key == active else ""
            cls = "fy-nav__item fy-expert-only" if expert else "fy-nav__item"
            b = f'<span class="fy-badge" aria-label="{blabel}">{badge}</span>' if badge else ""
            nav_html.append(f'<a class="{cls}" href="#{key}"{cur}>{icon(ic)}<span class="fy-nav__label">{label}</span>{b}</a>')
    foot = "".join(
        f'<a class="fy-nav__item" href="#{k}"{" aria-current=\"page\"" if k == active else ""}>{icon(ic)}<span class="fy-nav__label">{lbl}</span></a>'
        for k, lbl, ic in FOOT)
    side = (f'<nav class="fy-side" aria-label="Main">'
            f'<a class="fy-brand" href="#home"><span class="fy-brand__mark" aria-hidden="true">f</span><span class="fy-brand__word">Fyniq</span></a>'
            f'<div class="fy-nav">{"".join(nav_html)}</div>'
            f'<div class="fy-side__foot fy-nav">{foot}</div></nav>')
    mode = settings.get("mode", "expert")
    # PRD-001 FR-PPL-02: the Finance | People switch, for people with rights in Fyniq and Nemura
    workspace = ('<div class="fy-segmented fy-from-medium" role="radiogroup" aria-label="Workspace">'
                 f'<button type="button" role="radio" aria-checked="true">{icon("landmark")}Finance</button>'
                 f'<button type="button" role="radio" aria-checked="false">{icon("users")}People</button></div>'
                 if settings.get("workspace") else "")
    seg = (f'<div class="fy-segmented" role="radiogroup" aria-label="Experience mode">'
           f'<button type="button" role="radio" aria-checked="{"true" if mode == "basic" else "false"}">Basic</button>'
           f'<button type="button" role="radio" aria-checked="{"true" if mode == "expert" else "false"}">Expert</button></div>')
    top = (f'<header class="fy-top">'
           f'<a class="fy-brand" href="#home" aria-label="Fyniq home"><span class="fy-brand__mark" aria-hidden="true">f</span></a>'
           f'<span class="fy-top__title fy-truncate">{title}</span>'
           f'<button class="fy-org" type="button" aria-label="Switch organisation: Cognizhi Pte Ltd"><span class="fy-org__logo" aria-hidden="true">C</span><span class="fy-from-expanded">Cognizhi Pte Ltd</span>{icon("chevron-down")}</button>'
           f'<button class="fy-search fy-from-expanded" type="button">{icon("search")}<span>Search or jump to…</span><span class="fy-kbd">⌘K</span></button>'
           f'<div class="fy-top__actions">{workspace}'
           f'<button class="fy-iconbtn fy-until-expanded" type="button" aria-label="Search">{icon("search", "fy-i--20")}</button>'
           f'<span class="fy-from-medium">{seg}</span>'
           f'<button class="fy-modepill fy-only-compact" type="button" aria-label="Experience mode: {mode.title()}. Change mode">{mode.title()}</button>'
           f'<button class="fy-iconbtn fy-from-expanded" type="button" aria-label="Theme">{icon("sun-moon", "fy-i--20")}</button>'
           f'<button class="fy-iconbtn" type="button" aria-label="Notifications, 3 unread">{icon("bell", "fy-i--20")}<span class="fy-badge fy-badge--dot" aria-hidden="true"></span></button>'
           f'<button class="fy-ask" type="button" aria-label="Ask the AI assistant">{icon("sparkles")}<span class="fy-from-medium">Ask</span></button>'
           f'<span class="fy-avatar fy-from-medium" aria-label="Mei Lin Tan">ML</span>'
           f'</div></header>')
    slot3 = active if active not in ("home", "inbox", "banking") and active in {n[0] for n in NAV} else "sales"
    items = []
    for key in BOTTOM_FIXED[:2] + [slot3] + BOTTOM_FIXED[3:]:
        n = next(n for n in NAV if n[0] == key)
        cur = ' aria-current="page"' if key == active else ""
        b = f'<span class="fy-badge" aria-label="{n[4]}">{n[3]}</span>' if n[3] else ""
        items.append(f'<a class="fy-bottom__item" href="#{key}"{cur}>{icon(n[2])}<span>{n[1]}</span>{b}</a>')
    more_cur = ' aria-current="page"' if active not in BOTTOM_FIXED + [slot3] else ""
    items.append(f'<a class="fy-bottom__item" href="#more"{more_cur}>{icon("layout-grid")}<span>More</span></a>')
    bottom = f'<nav class="fy-bottom" aria-label="Main">{"".join(items)}</nav>'
    ai_attr = ' data-ai="open"' if ai else ""
    return (f'<div class="fy-app"{ai_attr}>{side}{top}<main class="fy-main" id="main">{main}</main>'
            f'{ai}{bottom}</div>')


# Nemura (PRD-002 §10): key, label, icon, badge, badge label, group. "personal" is everyone's own
# area; "team" is the people team's (HR officer, HR administrator, Owner)
NEMURA_NAV = [
    ("me", "Home", "house", None, None, "personal"),
    ("my-leave", "My leave", "tree-palm", None, None, "personal"),
    ("my-pay", "My pay", "wallet", None, None, "personal"),
    ("my-requests", "My requests", "file-text", "1", "1 letter ready", "personal"),
    ("my-goals", "My goals", "target", None, None, "personal"),
    ("team-home", "People home", "layout-dashboard", None, None, "team"),
    ("people", "People", "users", None, None, "team"),
    ("leave", "Leave", "calendar-days", "3", "3 leave requests waiting", "team"),
    ("payroll", "Payroll", "banknote", None, None, "team"),
    ("letters", "Letters", "mail", "2", "2 letters to issue", "team"),
    ("hiring", "Hiring", "user-plus", None, None, "team"),
    ("performance", "Performance", "trending-up", None, None, "team"),
    ("onboarding", "Onboarding", "list-checks", None, None, "team"),
]
NEMURA_FOOT = [("settings", "Settings", "settings"), ("help", "Help", "life-buoy")]
NEMURA_PERSONA = {  # who the screen shows, their initials, and the compact tab bar
    "employee": ("Daniel Lim", "DL", ["me", "my-leave", "my-pay", "my-requests"]),
    "hr": ("Sarah Koh", "SK", ["team-home", "people", "leave", "letters"]),
    "admin": ("Mei Lin Tan", "ML", ["team-home", "people", "leave", "payroll"]),
}


def shell_nemura(screen, settings, main, ai):
    persona = settings.get("persona", "hr")
    who, initials, tabs = NEMURA_PERSONA[persona]
    active = settings.get("nav") or ("me" if persona == "employee" else "team-home")
    title = settings.get("title") or screen["title"]
    groups = [("personal", "For you")] + ([] if persona == "employee" else [("team", "People team")])
    nav_html = []
    for group, glabel in groups:
        nav_html.append(f'<div class="fy-nav__group fy-eyebrow">{glabel}</div>')
        for key, label, ic, badge, blabel, g in NEMURA_NAV:
            if g != group:
                continue
            cur = ' aria-current="page"' if key == active else ""
            b = f'<span class="fy-badge" aria-label="{blabel}">{badge}</span>' if badge else ""
            nav_html.append(f'<a class="fy-nav__item" href="#{key}"{cur}>{icon(ic)}<span class="fy-nav__label">{label}</span>{b}</a>')
    foot = "".join(
        f'<a class="fy-nav__item" href="#{k}"{" aria-current=\"page\"" if k == active else ""}>{icon(ic)}<span class="fy-nav__label">{lbl}</span></a>'
        for k, lbl, ic in NEMURA_FOOT if persona != "employee" or k == "help")
    side = (f'<nav class="fy-side" aria-label="Main">'
            f'<a class="fy-brand" href="#me"><span class="fy-brand__mark" aria-hidden="true">n</span><span class="fy-brand__word">nemura</span></a>'
            f'<div class="fy-nav">{"".join(nav_html)}</div>'
            f'<div class="fy-side__foot fy-nav">{foot}</div></nav>')
    switch = ""
    if settings.get("integrated"):
        switch = ('<nav class="nm-workspace fy-from-medium" aria-label="Workspace">'
                  f'<a href="#finance">{icon("landmark")}Finance</a>'
                  f'<a href="#people" aria-current="page">{icon("users")}People</a></nav>')
    top = (f'<header class="fy-top">'
           f'<a class="fy-brand" href="#me" aria-label="Nemura home"><span class="fy-brand__mark" aria-hidden="true">n</span></a>'
           f'<span class="fy-top__title fy-truncate">{title}</span>'
           f'<button class="fy-org" type="button" aria-label="Switch organisation: Cognizhi Pte Ltd"><span class="fy-org__logo" aria-hidden="true">C</span><span class="fy-from-expanded">Cognizhi Pte Ltd</span>{icon("chevron-down")}</button>'
           f'<button class="fy-search fy-from-expanded" type="button">{icon("search")}<span>Search people, letters, policies…</span><span class="fy-kbd">⌘K</span></button>'
           f'<div class="fy-top__actions">{switch}'
           f'<button class="fy-iconbtn fy-until-expanded" type="button" aria-label="Search">{icon("search", "fy-i--20")}</button>'
           f'<button class="fy-iconbtn fy-from-expanded" type="button" aria-label="Theme">{icon("sun-moon", "fy-i--20")}</button>'
           f'<button class="fy-iconbtn" type="button" aria-label="Notifications, 2 unread">{icon("bell", "fy-i--20")}<span class="fy-badge fy-badge--dot" aria-hidden="true"></span></button>'
           f'<button class="fy-ask" type="button" aria-label="Ask the people assistant">{icon("sparkles")}<span class="fy-from-medium">Ask</span></button>'
           f'<span class="fy-avatar fy-from-medium" aria-label="{who}">{initials}</span>'
           f'</div></header>')
    items = []
    for key in tabs:
        n = next(n for n in NEMURA_NAV if n[0] == key)
        cur = ' aria-current="page"' if key == active else ""
        b = f'<span class="fy-badge" aria-label="{n[4]}">{n[3]}</span>' if n[3] else ""
        label = n[1].replace("My ", "").capitalize() if persona == "employee" else n[1]
        items.append(f'<a class="fy-bottom__item" href="#{key}"{cur}>{icon(n[2])}<span>{label}</span>{b}</a>')
    more_cur = ' aria-current="page"' if active not in tabs else ""
    items.append(f'<a class="fy-bottom__item" href="#more"{more_cur}>{icon("layout-grid")}<span>More</span></a>')
    bottom = f'<nav class="fy-bottom" aria-label="Main">{"".join(items)}</nav>'
    ai_attr = ' data-ai="open"' if ai else ""
    return (f'<div class="fy-app"{ai_attr}>{side}{top}<main class="fy-main" id="main">{main}</main>'
            f'{ai}{bottom}</div>')


# --- canvas ----------------------------------------------------------------------------------

def dc_page(title, body, props, preview, script_vals, product="fyniq"):
    cfg = PRODUCTS[product]
    data = json.dumps({**props, "$preview": preview}, ensure_ascii=False).replace("'", "&#39;")
    return f"""<!doctype html>
<html lang="en-SG">
<head>
<meta charset="utf-8">
<title>{title}</title>
<script src="./support.js"></script>
<link rel="stylesheet" href="kit/{cfg['kit']}">
</head>
<body>
<x-dc>
<helmet>
<link rel="stylesheet" href="{cfg['fonts']}">
<style>body{{margin:0}}</style>
</helmet>
{body}
</x-dc>
<script type="text/x-dc" data-dc-script data-props='{data}'>
class Component extends DCLogic {{
  renderVals() {{
    return {script_vals};
  }}
}}
</script>
</body>
</html>
"""


def frame_heights(settings):
    h = settings.get("heights", {})
    out = {}
    for key, _, w, dh, cls in FRAMES:
        want = h.get(cls, 0)
        if key == "fold-closed":
            want = int(want * 1.08)
        out[key] = max(dh, want)
    return out


def load_inventory():
    return json.loads(INVENTORY.read_text(encoding="utf-8"))


def cmd_canvas(args):
    if not args:
        raise SystemExit("usage: build.py canvas <id> [--ds URL] [--only File,…]")
    cid, ds, only = args[0], None, None
    if "--ds" in args:
        ds = args[args.index("--ds") + 1]
    if "--only" in args:
        only = set(args[args.index("--only") + 1].split(","))
    inv = load_inventory()
    canvas = next((c for c in inv["canvases"] if c["id"] == cid), None)
    if not canvas:
        raise SystemExit(f"✖ no canvas `{cid}` in inventory.json")
    product = canvas.get("product", "fyniq")
    cfg = PRODUCTS[product]
    screens = [s for s in inv["screens"] if s["canvas"] == cid and (SRC / f"{s['file']}.html").exists()
               and (only is None or s["file"] in only)]
    if not screens:
        raise SystemExit(f"✖ no fragments in src/ for canvas `{cid}`")
    classes = known_classes(product)
    bad = [e for s in screens for e in lint(SRC / f"{s['file']}.html", classes)[0]]
    if bad:
        print("\n".join(f"✖ {e}" for e in bad))
        raise SystemExit("✖ fix the fragments first (build.py check)")
    out = OUT / cid / "project"
    if out.exists():
        for f in out.rglob("*"):
            if f.is_file():
                f.unlink()
    (out / "kit").mkdir(parents=True, exist_ok=True)
    (out / f"kit/{cfg['kit']}").write_text((KIT / cfg["kit"]).read_text(encoding="utf-8"), encoding="utf-8")

    pages, boards, order, notes, page_y = [], {}, [], {}, {}
    for s in screens:
        settings, main, ai = read_fragment(SRC / f"{s['file']}.html")
        mode = settings.get("mode") or ("basic" if s.get("mode") == "basic" else "expert")
        settings["mode"] = mode
        main, ai = expand_icons(main), expand_icons(ai)
        if settings.get("layout") == "bare":
            inner = f'<div class="fy-bare-host">{main}</div>'
        else:
            inner = shell(s, settings, main, ai, product)
        motion = settings.get("motion", "on")
        root = (f'<div class="fy-device" data-theme="{{{{theme}}}}" data-mode="{{{{mode}}}}" '
                f'data-motion="{{{{motion}}}}" data-density="{{{{density}}}}">{inner}</div>')
        hts = frame_heights(settings)
        props = {
            "theme": {"editor": "enum", "options": ["light", "dark", "light-hc", "dark-hc"], "default": "light", "section": "Theme"},
            "mode": {"editor": "enum", "options": ["basic", "expert"], "default": mode, "section": "Theme"},
            "motion": {"editor": "enum", "options": ["on", "off", "reduce"], "default": motion, "section": "Theme"},
            "density": {"editor": "enum", "options": ["comfortable", "compact"], "default": "comfortable", "section": "Theme"},
        }
        vals = ("{ theme: this.props.theme ?? 'light', mode: this.props.mode ?? '" + mode +
                "', motion: this.props.motion ?? '" + motion + "', density: this.props.density ?? 'comfortable' }")
        src_name = f"{s['file']}.dc.html"
        (out / src_name).write_text(dc_page(f"{cfg['name']} · {s['title']}", root, props,
                                            {"width": 1440, "height": hts["desktop"]}, vals, product), encoding="utf-8")
        page_id = re.sub(r"[^A-Za-z0-9_-]+", "-", s.get("module", "Screens")).strip("-").lower() or "screens"
        if page_id not in [p["id"] for p in pages]:
            pages.append({"id": page_id, "name": s.get("module", "Screens")})
            page_y[page_id] = 0
        y = page_y[page_id] + TITLE_SPACE
        row_h = max(hts.values())
        notes[f"t-{s['file']}"] = {"x": 0, "y": y - 260, "text": f"{s['title']}", "kind": "title1",
                                   "maxW": sum(f[2] for f in FRAMES) + GAP_X * 4, "page": page_id}
        reqs = ", ".join(s.get("requirements", [])[:8])
        notes[f"n-{s['file']}"] = {"x": 0, "y": y - 120, "text": f"{s['id']} · {s.get('route', '')} · {s.get('phase', '')} · {mode} mode · {reqs}",
                                   "size": "s", "w": 1400, "maxH": 80, "page": page_id}
        for ti, (theme, tlabel) in enumerate(THEMES):
            x = 0
            ry = y + ti * (row_h + GAP_ROW)
            for key, label, w, _, cls in FRAMES:
                h = hts[key]
                if key == "desktop" and theme == "light":
                    name = src_name
                else:
                    name = f"{s['file']}.{key}-{theme}.dc.html"
                    body = (f'<div style="width: {w}px; height: {h}px; overflow: hidden; position: relative">'
                            f'<dc-import name="{s["file"]}" theme="{theme}" mode="{mode}" motion="{motion}" '
                            f'hint-size="{w}px,{h}px"></dc-import></div>')
                    (out / name).write_text(dc_page(f"{cfg['name']} · {s['title']} · {label} · {tlabel}", body, {},
                                                    {"width": w, "height": h}, "{}", product), encoding="utf-8")
                boards[name] = {"x": x, "y": ry, "w": w, "h": h, "title": f"{s['title']} · {label} · {tlabel}", "page": page_id}
                order.append(name)
                x += w + GAP_X
        page_y[page_id] = y + 2 * row_h + GAP_ROW + GAP_BLOCK
    now = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    index = {"v": 3, "createdOnFiles": {"v": 1, "at": now}, "title": canvas["title"],
             "launch": {"view": "canvas", "page": pages[0]["id"]}, "pages": pages, "boards": boards,
             "order": order, "notes": notes, "designSystems": []}
    if ds:
        ns, dsd = cfg["namespace"], cfg["ds_dir"]
        index["designSystems"].append({"title": cfg["name"], "namespace": ns, "artifact": ds, "version": None, "copiedAt": now})
        if not (OUT / f"{dsd}/project/tokens.json").exists():
            cmd_ds([] if product == "fyniq" else [product])
        (out / f"ds/{ns}").mkdir(parents=True, exist_ok=True)
        (out / f"ds/{ns}/tokens.json").write_text((OUT / f"{dsd}/project/tokens.json").read_text(encoding="utf-8"), encoding="utf-8")
    (out / "canvas.json").write_text(json.dumps(index, indent=1, ensure_ascii=False), encoding="utf-8")
    files = sorted(p.relative_to(out.parent).as_posix() for p in out.rglob("*") if p.is_file())
    print(f"✔ out/{cid}/project: {len(screens)} screens, {len(boards)} artboards, {len(files)} files, "
          f"{sum(p.stat().st_size for p in out.rglob('*') if p.is_file()) // 1024} KB")
    if len(files) > 199:
        print("⚠ more than 199 files: the canvas may not load them all; split the canvas")
    return 0


def device_markup(screen, theme, product="fyniq"):
    """The screen as plain HTML (no canvas runtime), for local previews."""
    settings, main, ai = read_fragment(SRC / f"{screen['file']}.html")
    mode = settings.get("mode") or ("basic" if screen.get("mode") == "basic" else "expert")
    settings["mode"] = mode
    main, ai = expand_icons(main), expand_icons(ai)
    inner = f'<div class="fy-bare-host">{main}</div>' if settings.get("layout") == "bare" else shell(screen, settings, main, ai, product)
    return (f'<div class="fy-device" data-theme="{theme}" data-mode="{mode}" data-motion="off" '
            f'data-density="comfortable">{inner}</div>'), frame_heights(settings)


def cmd_preview(args):
    """out/preview/<File>/<frame>-<theme>.html: each frame as a standalone page (motion off), for a
    local look with a browser or headless Chrome before publishing. Not a substitute for the canvas."""
    inv = load_inventory()
    names = args or [s["file"] for s in inv["screens"] if (SRC / f"{s['file']}.html").exists()]
    for name in names:
        s = next(x for x in inv["screens"] if x["file"] == name)
        product = canvas_product(s["canvas"], inv)
        css, fonts = (KIT / PRODUCTS[product]["kit"]).resolve().as_uri(), PRODUCTS[product]["fonts"]
        d = OUT / "preview" / name
        d.mkdir(parents=True, exist_ok=True)
        for theme, _ in THEMES:
            body, hts = device_markup(s, theme, product)
            for key, label, w, _, _ in FRAMES:
                h = hts[key]
                page = (f'<!doctype html><html lang="en-SG"><head><meta charset="utf-8"><title>{s["title"]} · {label}</title>'
                        f'<link rel="stylesheet" href="{fonts}"><link rel="stylesheet" href="{css}">'
                        '<style>body{margin:0;background:#777}.frame{position:relative;overflow:hidden}'
                        '.frame>.fy-device,.frame .fy-app{min-height:100%;height:100%}'
                        '.frame .fy-side{height:auto;max-height:none}.frame .fy-ai{height:auto}</style></head>'
                        f'<body><div class="frame" style="width:{w}px;height:{h}px">{body}</div></body></html>')
                (d / f"{key}-{theme}.html").write_text(page, encoding="utf-8")
        print(f"✔ out/preview/{name}: {len(FRAMES) * len(THEMES)} pages")
    return 0


def cmd_shots(args):
    """PNG screenshots of a screen's preview pages with headless Chrome (local check only).
    build.py shots <File> [frame-theme …]   e.g. shots Home desktop-light phone-dark"""
    import shutil
    import subprocess
    if not args:
        raise SystemExit("usage: build.py shots <File> [desktop-light phone-dark …]")
    name, wanted = args[0], args[1:] or ["desktop-light", "phone-dark", "fold-open-light"]
    cmd_preview([name])
    chrome = shutil.which("google-chrome") or shutil.which("chromium") or shutil.which("chromium-browser")
    if not chrome:
        raise SystemExit("✖ no Chrome or Chromium on PATH")
    d = OUT / "preview" / name
    for key in wanted:
        page = d / f"{key}.html"
        m = re.search(r'class="frame" style="width:(\d+)px;height:(\d+)px"', page.read_text(encoding="utf-8"))
        w, h = int(m.group(1)), int(m.group(2))
        png = d / f"{key}.png"
        subprocess.run([chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--hide-scrollbars",
                        "--virtual-time-budget=4000", f"--window-size={max(w, 800)},{h + 100}",
                        f"--screenshot={png}", page.as_uri()], capture_output=True, timeout=90)
        print(f"✔ {png.relative_to(ROOT)} ({w}×{h} frame)")
    return 0


def cmd_heights(args):
    """Measure each fragment's content height per device class with headless Chrome and suggest
    the `heights` setting (content + 40 px, rounded up to 50). --write stores it in the fragment.
    Measures the page (or bare screen) and the compact bottom bar; a dialog over the page is not
    measured, so set those heights by eye with `shots`. Local check only.
    build.py heights <File>… [--write]"""
    import math
    import shutil
    import subprocess
    names = [a for a in args if not a.startswith("--")]
    if not names:
        raise SystemExit("usage: build.py heights <File>… [--write]")
    chrome = shutil.which("google-chrome") or shutil.which("chromium") or shutil.which("chromium-browser")
    if not chrome:
        raise SystemExit("✖ no Chrome or Chromium on PATH")
    probe = ("<script>window.addEventListener('load',function(){var b=0;"
             "document.querySelectorAll('.fy-page, .fy-bare, .fy-actionbar').forEach(function(e){"
             "b=Math.max(b,e.getBoundingClientRect().bottom)});"
             "var t=document.querySelector('.fy-bottom');var tb=t?t.getBoundingClientRect().height:0;"
             "document.body.setAttribute('data-h',Math.ceil(b+tb))});</script>")
    # The narrowest frame of each class is the tallest
    frames = {"compact": "fold-closed-light", "medium": "fold-open-light", "expanded": "desktop-light"}
    cmd_preview(names)
    for name in names:
        got = {}
        for cls, key in frames.items():
            page = (OUT / "preview" / name / f"{key}.html").read_text(encoding="utf-8")
            page = re.sub(r'(class="frame" style="width:\d+px;height:)\d+px', r"\g<1>8000px", page)
            tmp = OUT / "preview" / name / f"measure-{key}.html"
            tmp.write_text(page.replace("</body>", probe + "</body>"), encoding="utf-8")
            dom = subprocess.run([chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--virtual-time-budget=3000",
                                  "--dump-dom", tmp.as_uri()], capture_output=True, text=True, timeout=90).stdout
            m = re.search(r'<body data-h="(\d+)"', dom)
            got[cls] = int(m.group(1)) if m else None
        sug = {c: int(math.ceil((h + 40) / 50.0) * 50) for c, h in got.items() if h}
        f = SRC / f"{name}.html"
        text = f.read_text(encoding="utf-8")
        meta = json.loads(re.match(r"<!-- fyniq-screen (\{.*?\}) -->", text).group(1))
        print(f"{name}: measured {got} · declared {meta.get('heights')} · suggested {sug}")
        if "--write" in args and len(sug) == 3:
            meta["heights"] = sug
            text = re.sub(r"<!-- fyniq-screen \{.*?\} -->", lambda _: "<!-- fyniq-screen " + json.dumps(meta, ensure_ascii=False) + " -->", text, count=1)
            f.write_text(text, encoding="utf-8")
    return 0


def cmd_files(args):
    """The `files` map for publishing out/<id>/project with the Artifact tool (canvas.json excluded:
    it is the call's file_path). build.py files <id> [--ds URL]"""
    cid = args[0]
    ds = args[args.index("--ds") + 1] if "--ds" in args else None
    root = OUT / cid
    files = {p.relative_to(root).as_posix(): p.relative_to(root).as_posix()
             for p in sorted((root / "project").rglob("*")) if p.is_file() and p.name != "canvas.json"}
    if "--list" in args:
        print(json.dumps([{"path": k} for k in files], separators=(",", ":")))
        return 0
    if ds:
        files[f"project/ds/{PRODUCTS[canvas_product(cid)]['namespace']}/tokens.json"] = {"artifact": ds, "path": "project/tokens.json"}
    print(json.dumps(files, separators=(",", ":")))
    return 0


def cmd_status(args):
    inv = load_inventory()
    for c in inv["canvases"]:
        ss = [s for s in inv["screens"] if s["canvas"] == c["id"]]
        done = [s for s in ss if (SRC / f"{s['file']}.html").exists()]
        print(f"{c['id']:<18} {len(done):>3}/{len(ss):<3} {c['title']}")
        for s in ss:
            print(f"   {'✔' if s in done else '·'} {s['file']:<28} {s['title']}")
    return 0


# --- design system ---------------------------------------------------------------------------

USAGE_NEMURA = {
    "ink": "The darkest line; light in dark mode.",
    "nav-bg": "The navigation rail and the compact tab bar (top of the gradient).",
    "nav-bg-2": "The navigation rail and tab bar (bottom of the gradient).",
    "nav-fg": "Navigation labels on the rail.",
    "nav-muted": "Group labels and idle items on the rail.",
    "nav-active-bg": "The raised pill of the current page on the rail and tab bar.",
    "nav-active-fg": "Text on the current page's pill.",
    "tile-fg": "Text on action tiles.",
    "tile-icon-fg": "Icons on a tile's icon disc.",
    "tile-marigold": "Tile tint: the main thing to do now (apply for leave); public holidays.",
    "tile-mint": "Tile tint: pay and payslips; family leave.",
    "tile-sky": "Tile tint: letters and documents; annual leave.",
    "tile-blush": "Tile tint: help and support; sick leave.",
    "tile-sand": "Tile tint: everything else.",
    "tile-marigold-deep": "Icon disc and dot for marigold tiles and public holidays.",
    "tile-mint-deep": "Icon disc and dot for mint tiles and family leave.",
    "tile-sky-deep": "Icon disc and dot for sky tiles and annual leave; avatars.",
    "tile-blush-deep": "Icon disc and dot for blush tiles and sick leave.",
    "tile-sand-deep": "Icon disc and dot for neutral tiles.",
}

USAGE = {
    "background": "Page background.", "surface-1": "Cards, tables, panels.", "surface-2": "Subtle panels, table headers, sidebar.",
    "surface-3": "Hover and selected rows.", "text-primary": "Body text and figures.", "text-secondary": "Labels and supporting text.",
    "text-muted": "Help text, captions, axis labels.", "text-on-accent": "Text on brand or AI fills.",
    "border": "Decorative dividers (not control boundaries).", "border-strong": "Control boundaries and totals rules (3:1).",
    "primary": "Brand teal: the one primary action per view, selection.", "primary-hover": "Primary action hover.",
    "primary-foreground": "Text on primary.", "primary-subtle": "Selected navigation, info banners.",
    "primary-subtle-foreground": "Text on primary-subtle.", "link": "Links.", "ring": "Focus ring.",
    "ai": "AI violet: only AI-originated content, always with the AI label.", "ai-foreground": "Text on AI fills.",
    "ai-subtle": "AI message and proposal backgrounds, the AI badge.", "ai-subtle-foreground": "Text on ai-subtle.",
    "ai-border": "Border of AI cards.", "destructive": "Void, revoke and other destructive actions.",
    "destructive-foreground": "Text on destructive fills.", "amount-positive": "Optional colouring of positive amounts.",
    "amount-negative": "Optional colouring of negative amounts.", "chart-surface": "Chart plot background.",
    "chart-grid": "Chart gridlines.", "chart-other": "The 'Other' series.",
}


def cmd_ds(args):
    product = args[0] if args else "fyniq"
    cfg = PRODUCTS[product]
    th, _ = themes(cfg["tokens"])
    names = {"light": "Light", "dark": "Dark", "light-hc": "High contrast", "dark-hc": "High contrast dark"}
    colour = [k for k, v in th["light"].items() if k.startswith("--") and re.match(r"(#|oklch\()", v.strip())]
    tokens = []
    for k in colour:
        n = k[2:]
        if n.startswith("seq-") or n.startswith("div-") or n.startswith("viz-"):
            usage = "Data visualisation scale (dataviz reference palette)."
        elif n.startswith("status-"):
            usage = "Status chips and banners, always with an icon and a label." if not n.endswith("subtle") else "Background of status chips and banners."
        elif n.startswith("chart-") and n[6:].isdigit():
            usage = f"Categorical series {n[6:]}, in fixed order; never for text."
        else:
            usage = USAGE.get(n, USAGE_NEMURA.get(n, "") if product == "nemura" else "")
        tokens.append({"name": n, "value": {t: th[t][k].strip() for t in names}, "usage": usage})
    root = th["light"]
    spacing = [{"name": k[2:], "value": root[k], "usage": "4 px grid."} for k in root if k.startswith("--space-")]
    radius = [{"name": k[2:], "value": root[k], "usage": u} for k, u in
              (("--radius-s", "Chips, inputs."), ("--radius-m", "Buttons."), ("--radius-l", "Cards, dialogs, panels."))]
    shadow = [{"name": k[2:], "value": {t: th[t][k].strip() for t in ("light", "dark")}, "usage": u} for k, u in
              (("--shadow-1", "Resting cards."), ("--shadow-2", "Raised cards, the pulse card."), ("--shadow-3", "Dialogs, popovers, panels."))]
    sizes = [("caption", "--fs-xs", 16, 500), ("table-compact", "--fs-sm", 18, 400), ("body", "--fs-base", 21, 400),
             ("reading", "--fs-md", 24, 400), ("section-title", "--fs-lg", 24, 600), ("page-title", "--fs-xl", 28, 600),
             ("stat", "--fs-2xl", 34, 600), ("pulse", "--fs-3xl", 40, 600)]
    rem = lambda v: f"{round(float(v.replace('rem', '')) * 16)}px"
    families = ({"ui": "Inter, ui-sans-serif, system-ui, sans-serif", "code": "\"JetBrains Mono\", ui-monospace, monospace"}
                if product == "fyniq" else
                {"ui": "\"Funnel Sans\", ui-sans-serif, system-ui, sans-serif",
                 "display": "\"Funnel Display\", \"Funnel Sans\", ui-sans-serif, sans-serif",
                 "code": "\"Spline Sans Mono\", ui-monospace, monospace"})
    out = {
        "name": cfg["name"], "version": 1,
        "meta": {"source": "github", "repo": "cognizhi/fyniq", "paths": {"tokens": [cfg["tokens"].relative_to(ROOT).as_posix()]},
                 "synced": datetime.date.today().isoformat()},
        "color": {"themes": [{"id": t, "name": n} for t, n in names.items()], "tokens": tokens},
        "type": {"fonts": [], "families": families,
                 "groups": [{"name": "Interface", "family": "ui", "styles": [
                     {"name": n, "fontSize": rem(root[t]), "lineHeight": f"{lh}px", "fontWeight": w, "usage": ""} for n, t, lh, w in sizes]},
                     {"name": "Code", "family": "code", "styles": [{"name": "code", "fontSize": "12.5px", "lineHeight": "18px", "fontWeight": 400,
                                                                    "usage": "References, IDs, account codes."}]}]},
        "spacing": {"tokens": spacing}, "radius": {"tokens": radius}, "shadow": {"tokens": shadow},
    }
    if product == "nemura":
        out["type"]["groups"].insert(1, {"name": "Display", "family": "display", "styles": [
            {"name": "hero", "fontSize": "48px", "lineHeight": "50px", "fontWeight": 800, "usage": "Greeting and hero figures."},
            {"name": "page-title-display", "fontSize": "38px", "lineHeight": "42px", "fontWeight": 800, "usage": "Page titles (expanded)."},
            {"name": "tile-title", "fontSize": "19px", "lineHeight": "22px", "fontWeight": 800, "usage": "Action tiles."}]})
        radius.append({"name": "radius-pill", "value": root["--radius-pill"], "usage": "Filter pills, counters."})
        shadow.append({"name": "shadow-raised", "value": {t: th[t]["--shadow-raised"].strip() for t in ("light", "dark")},
                       "usage": "Raised elements: action tiles on hover, primary buttons, the current page in the rail."})
    d = OUT / f"{cfg['ds_dir']}/project"
    (d / "components/Cover").mkdir(parents=True, exist_ok=True)
    (d / "tokens.json").write_text(json.dumps(out, indent=1, ensure_ascii=False), encoding="utf-8")
    (d / "README.md").write_text((cfg["ds_src"] / "README.md").read_text(encoding="utf-8"), encoding="utf-8")
    (d / "components/Cover/preview.html").write_text((cfg["ds_src"] / "Cover.html").read_text(encoding="utf-8"), encoding="utf-8")
    now = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    index = {"v": 3, "layout": "files", "createdOnFiles": {"v": 1, "at": now}, "title": cfg["name"], "namespace": cfg["name"],
             "libraries": [], "sections": {}, "groups": [], "assetGroups": {}, "blobs": {}, "docs": {"sections": []},
             "lastChange": {"by": "Claude", "at": now, "via": "Claude Code · cognizhi/fyniq",
                            "note": f"Built from docs/design-system/{cfg['namespace']} (tokens.css, MASTER.md) and "
                                    f"docs/design/screens/{cfg['ds_src'].name}"}}
    (d / "design-system.json").write_text(json.dumps(index, indent=1), encoding="utf-8")
    print(f"✔ out/{cfg['ds_dir']}/project: tokens.json ({len(tokens)} colours × 4 themes, {len(spacing)} spacing, 3 radii, "
          f"3 shadows, 9 text styles), README.md, components/Cover/preview.html, design-system.json")
    return 0


COMMANDS = {"kit": cmd_kit, "check": cmd_check, "canvas": cmd_canvas, "status": cmd_status, "ds": cmd_ds, "preview": cmd_preview, "shots": cmd_shots, "heights": cmd_heights, "files": cmd_files}

if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] not in COMMANDS:
        print(__doc__.strip())
        sys.exit(2)
    sys.exit(COMMANDS[sys.argv[1]](sys.argv[2:]))

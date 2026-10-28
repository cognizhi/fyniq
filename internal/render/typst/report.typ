// The render service's report source (FR-RPT-05; p1-reports task 3.2): it lays out report.json, which Go wrote from a
// financial statement (internal/api), every figure already written, so nothing here computes or rounds (RND-DISPLAY).
// Every string is placed as text, never read as markup or code. PDF/A-2a and PDF/UA-1 come from the compile flags;
// this source gives the title, language, headings and the table's header row they need, the header repeated on every
// page. More than five columns turn the page to landscape.

#let r = json("report.json")
#let accent = rgb("#0F766E") // the design system's brand teal at a shade that reads on white (5.4:1)
#let ink = rgb("#1f2937")
#let muted = rgb("#4b5563")
#let rule = rgb("#d1d5db")
#let n = r.columns.len()
#let wide = n > 8 // a column a month: smaller type and tighter cells, so the accounts keep their room
#let at(x, key, default: none) = x.at(key, default: default)

#set document(title: r.title, author: r.author)
#set text(font: "Inter", lang: "en", region: "SG", size: if wide { 6.8pt } else { 8.5pt }, fill: ink, number-width: "tabular")
#set par(leading: 0.55em, spacing: 0.8em)
#set page(
  paper: "a4",
  flipped: n > 5,
  margin: (top: 16mm, right: 14mm, bottom: 18mm, left: 14mm),
  footer: context {
    set text(size: 7pt, fill: muted)
    grid(
      columns: (1fr, auto),
      column-gutter: 12pt,
      r.footer,
      [Page #counter(page).display() of #counter(page).final().first()],
    )
  },
)
#show heading.where(level: 1): set text(size: 15pt, weight: "bold", fill: accent)
#show heading.where(level: 2): set text(size: 9.5pt, weight: "bold", fill: ink)

#text(size: 9.5pt, weight: "bold", r.author)
#heading(level: 1, r.title)
#for l in r.lines [
  #text(fill: muted, l) \
]
#v(4pt)

// The rows: a section's title across the table; a line or subtotal indented; a total or measure bold under a rule.
#let cells(x) = {
  if x.kind == "section" {
    return (table.cell(colspan: n + 1, inset: (x: 4pt, top: 8pt, bottom: 3pt), text(weight: "bold", fill: accent, x.label)),)
  }
  let strong = x.kind in ("total", "measure")
  let w = if strong { "bold" } else { "regular" }
  let line = if strong { (top: 0.6pt + ink) } else { (bottom: 0.3pt + rule) }
  let indent = if x.kind in ("line", "subtotal") { if wide { 5pt } else { 8pt } } else { 0pt }
  let code = at(x, "code", default: "")
  let label = if code != "" { [#text(fill: muted, code)#h(5pt)#x.label] } else { x.label }
  (table.cell(stroke: line, pad(left: indent, text(weight: w, label))),) + at(x, "cells", default: ()).map(c => table.cell(stroke: line, text(weight: w, c)))
}

#table(
  columns: (1fr,) + (auto,) * n,
  align: (x, y) => if x == 0 { left + horizon } else { right + horizon },
  stroke: none,
  inset: (x: if wide { 2.5pt } else { 4pt }, y: 3pt),
  table.header(
    repeat: true,
    table.cell(stroke: (bottom: 0.8pt + accent), text(weight: "bold", r.label)),
    ..r.columns.map(c => table.cell(stroke: (bottom: 0.8pt + accent), text(weight: "bold", c))),
  ),
  ..r.rows.map(cells).flatten(),
)

#for note in at(r, "notes", default: ()) {
  v(6pt)
  heading(level: 2, note.title)
  list(..note.items.map(i => text(i)))
}

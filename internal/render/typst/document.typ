// The one Typst source the render service owns (PRD-001 §8.25, FR-TPL-02): it lays out layout.json, which Go built
// from a template version and a document's data (internal/render). Every string is written as text, never read as
// markup or code, so a template holds no code; each IRAS particular a line carries is marked with <particular>
// metadata, which the service queries to prove the particulars are in the document (FR-TPL-03). PDF/A-2a and
// PDF/UA-1 come from the compile flags; this source gives the title, language, headings, table headers and alt text
// they need.

#let d = json("layout.json")
#let accent = rgb(d.accent)
#let ink = rgb("#1f2937")
#let muted = rgb("#4b5563")
#let rule = rgb("#d1d5db")

#let at(x, key, default: none) = x.at(key, default: default)
#let mark(p) = if p != none and p != "" [#metadata(p)<particular>]
#let marks(ps) = for p in ps { mark(p) }
#let footer = d.blocks.find(b => b.kind == "footer")

#set document(title: d.title, author: d.author)
#set text(font: d.font, lang: d.lang, region: d.region, size: 9.5pt, fill: ink)
#set par(leading: 0.62em, spacing: 0.9em)
#set page(
  paper: d.paper,
  margin: (top: d.margins_mm.at(0) * 1mm, right: d.margins_mm.at(1) * 1mm, bottom: d.margins_mm.at(2) * 1mm, left: d.margins_mm.at(3) * 1mm),
  footer: if footer != none {
    set text(size: 8pt, fill: muted)
    align(center, at(footer, "body", default: ""))
  },
)
#show heading.where(level: 1): set text(size: 18pt, weight: "bold", fill: accent)
#show heading.where(level: 2): set text(size: 9.5pt, weight: "bold", fill: accent)

#let items(b) = for it in at(b, "items", default: ()) {
  [#it.text#mark(at(it, "particular")) \ ]
}

#let pairs(rows) = grid(
  columns: (1fr, auto),
  column-gutter: 12pt,
  row-gutter: 5pt,
  ..rows.map(r => {
    let w = if at(r, "strong", default: false) { "bold" } else { "regular" }
    (
      text(weight: w, fill: if w == "bold" { ink } else { muted }, r.label),
      align(right, [#text(weight: w, r.value)#marks(at(r, "particulars", default: ()))]),
    )
  }).flatten(),
)

#let labelled(b, body) = {
  if at(b, "label", default: "") != "" { heading(level: 2, b.label) }
  body
}

#for b in d.blocks {
  if b.kind == "header" {
    grid(
      columns: (1fr, auto),
      align: (left + horizon, right + horizon),
      if at(b, "logo", default: false) { image("logo.png", height: 14mm, alt: d.author + " logo") },
      [#heading(level: 1, b.items.at(0).text)#mark(at(b.items.at(0), "particular"))],
    )
    v(6pt)
  } else if b.kind == "our_details" {
    block(text(size: 8.5pt, items(b)))
  } else if b.kind == "recipient" {
    block(labelled(b, items(b)))
  } else if b.kind == "document_details" {
    block(width: 60%, pairs(at(b, "rows", default: ())))
  } else if b.kind == "line_items" {
    let cols = at(b, "columns", default: ())
    let aligns = cols.map(c => if at(c, "right", default: false) { right } else { left })
    block[
      #table(
        columns: cols.enumerate().map(((i, c)) => if i == 0 { 1fr } else { auto }),
        align: (x, y) => aligns.at(x),
        stroke: (x, y) => (bottom: if y == 0 { 0.8pt + accent } else { 0.4pt + rule }),
        inset: (x: 5pt, y: 5pt),
        table.header(..cols.map(c => text(weight: "bold", c.title))),
        ..at(b, "cells", default: ()).flatten(),
      )
      #marks(at(b, "particulars", default: ()))
    ]
  } else if b.kind == "totals" {
    align(right, block(width: 55%, {
      pairs(at(b, "rows", default: ()))
      let note = at(b, "note")
      if note != none { v(4pt); text(size: 8.5pt, fill: muted, [#note.text#mark(at(note, "particular"))]) }
    }))
  } else if b.kind in ("payment_details", "notes") {
    if at(b, "body", default: "") != "" { block(labelled(b, at(b, "body", default: ""))) }
  } else if b.kind == "signature" {
    v(18mm)
    block(width: 45%, {
      line(length: 100%, stroke: 0.6pt + ink)
      text(size: 8.5pt, fill: muted, at(b, "label", default: ""))
    })
  } else if b.kind == "text" {
    if at(b, "body", default: "") != "" { block(at(b, "body", default: "")) }
  }
  v(4pt)
}

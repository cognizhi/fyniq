// The render service's paper source (FR-CS-05; p1-company-secretary task 4.1): it lays out paper.json, a resolution, a
// notice or minutes that Go filled in from the records (internal/domain/corporate). Every string is placed as text,
// never read as markup or code. PDF/A-2a and PDF/UA-1 come from the compile flags; this source gives the title,
// language and headings they need.

#let p = json("paper.json")
#let accent = rgb("#0F766E") // the design system's brand teal at a shade that reads on white (5.4:1)
#let ink = rgb("#1f2937")
#let muted = rgb("#4b5563")
// at is x's key, or default when it is missing or null (Go writes an empty list as null).
#let at(x, key, default: none) = {
  let v = x.at(key, default: none)
  if v == none { default } else { v }
}

#set document(title: p.title, author: p.author)
#set text(font: "Inter", lang: "en", region: "SG", size: 10pt, fill: ink)
#set par(leading: 0.65em, spacing: 1em)
#set page(
  paper: "a4",
  margin: (top: 20mm, right: 20mm, bottom: 20mm, left: 20mm),
  footer: context {
    set text(size: 7.5pt, fill: muted)
    grid(
      columns: (1fr, auto),
      column-gutter: 12pt,
      p.footer,
      [Page #counter(page).display() of #counter(page).final().first()],
    )
  },
)
#show heading.where(level: 1): set text(size: 14pt, weight: "bold", fill: accent)
#show heading.where(level: 2): set text(size: 10.5pt, weight: "bold", fill: ink)

// A banner, such as that the paper is a draft to review, boxed above everything.
#let banner = at(p, "banner", default: "")
#if banner != "" {
  block(width: 100%, inset: 6pt, stroke: 0.6pt + muted, text(size: 8.5pt, fill: muted, banner))
  v(6pt)
}

#align(center)[
  #text(size: 12pt, weight: "bold", p.author) \
  #for l in at(p, "lines", default: ()) [
    #text(fill: muted, l) \
  ]
]
#v(4pt)
#align(center, heading(level: 1, p.title))
#v(4pt)

// The sections: a heading, its paragraphs, then its numbered items.
#for s in p.sections {
  let h = at(s, "heading", default: "")
  if h != "" {
    heading(level: 2, h)
  }
  for para in at(s, "paragraphs", default: ()) {
    par(text(para))
  }
  let items = at(s, "items", default: ())
  if items.len() > 0 {
    enum(..items.map(i => text(i)))
  }
}

// The signatures: a line to sign above each name, two to a row, each kept whole on its page.
#let signatures = at(p, "signatures", default: ())
#if signatures.len() > 0 {
  v(16pt)
  grid(
    columns: (1fr, 1fr),
    column-gutter: 24pt,
    row-gutter: 20pt,
    ..signatures.map(s => block(breakable: false)[
      #v(22pt)
      #line(length: 100%, stroke: 0.5pt + ink)
      #text(weight: "bold", s.name) \
      #text(fill: muted, s.role) \
      #text(fill: muted)[Date:]
    ]),
  )
}

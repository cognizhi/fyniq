// A logo for documents and emails (FR-ORG-09): the organisation's logo, a cleaned SVG or a PNG named by the input
// "logo", drawn 96 points high, or narrower when that would be wider than 384 points, on a transparent page that fits
// it. At 144 pixels an inch that is 192 pixels high and at most 768 wide.
#set page(width: auto, height: auto, margin: 0pt, fill: none)
#let name = sys.inputs.at("logo")
#context {
  let natural = measure(image(name))
  let ratio = natural.width / natural.height
  let (w, h) = (96pt * ratio, 96pt)
  if w > 384pt { (w, h) = (384pt, 384pt / ratio) }
  image(name, width: w, height: h)
}

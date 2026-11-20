// Package dashboard is the configurable dashboard (PRD-001 FR-VIZ-09, FR-KPI-05; p2-reporting-planning design D9): a layout is a list of tiles a
// person or a role arranges on a twelve-column grid, each a KPI of the catalogue. A layout belongs to a role or to a person; a person's own wins
// over their role's, and the default is the KPIs the workbook led with. The layout holds no figure and no right: it names tiles, and each tile
// shows only what the viewer's own rights read, so a layout never widens what anyone sees.
package dashboard

import (
	"fmt"
	"slices"

	"github.com/cognizhi/fyniq/internal/domain/kpi"
)

// The limits of a layout.
const (
	Columns   = 12
	MaxTiles  = 24
	MaxHeight = 4
)

// The codes of a layout's rules.
const (
	CodeTile    = "DASH-01" // a tile that is not in the catalogue, or is placed twice
	CodeGrid    = "DASH-02" // a tile outside the grid
	CodeTooMany = "DASH-03" // too many tiles
)

// Tile is one placed tile.
type Tile struct {
	Tile string `json:"tile"` // a KPI's key
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
}

// Problem is one rule that failed.
type Problem struct{ Code, Field, Message, Fix string }

// Catalogue are the tiles that can be placed: each KPI of the catalogue, by its key.
func Catalogue() []string {
	out := make([]string, 0, len(kpi.Definitions))
	for _, d := range kpi.Definitions {
		out = append(out, d.Key)
	}
	return out
}

// Default is the layout a person gets before anyone has arranged one: the headline KPIs of the start-up, three to a row.
func Default() []Tile {
	keys := []string{"cash", "runway", "net_burn", "revenue", "gross_margin", "net_profit_ytd"}
	out := make([]Tile, 0, len(keys))
	for i, k := range keys {
		out = append(out, Tile{Tile: k, X: (i % 3) * 4, Y: (i / 3), W: 4, H: 1})
	}
	return out
}

// Validate checks a layout: every tile in the catalogue and placed once, inside the twelve columns, with a height of 1 to 4, at most 24 tiles.
func Validate(tiles []Tile) []Problem {
	var bad []Problem
	if len(tiles) > MaxTiles {
		bad = append(bad, Problem{CodeTooMany, "tiles", fmt.Sprintf("has %d tiles; a dashboard has up to %d", len(tiles), MaxTiles), "Remove a tile"})
	}
	known := Catalogue()
	seen := map[string]bool{}
	for i, t := range tiles {
		f := fmt.Sprintf("tiles[%d]", i)
		switch {
		case !slices.Contains(known, t.Tile):
			bad = append(bad, Problem{CodeTile, f + ".tile", fmt.Sprintf("%q is not a tile", t.Tile), "Choose a tile from the list the dashboard offers"})
		case seen[t.Tile]:
			bad = append(bad, Problem{CodeTile, f + ".tile", fmt.Sprintf("%q is placed twice", t.Tile), "Place each tile once"})
		}
		seen[t.Tile] = true
		if t.X < 0 || t.W < 1 || t.X+t.W > Columns || t.Y < 0 || t.H < 1 || t.H > MaxHeight {
			bad = append(bad, Problem{CodeGrid, f, "is outside the grid of 12 columns and rows 1 to 4 tall", "Keep x from 0, width 1 to 12 within the 12 columns, and height 1 to 4"})
		}
	}
	return bad
}

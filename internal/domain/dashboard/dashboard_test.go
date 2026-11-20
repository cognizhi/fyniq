package dashboard

import "testing"

// req: FR-VIZ-09
func TestALayoutNamesCatalogueTilesOnceInsideTheGrid(t *testing.T) {
	if p := Validate(Default()); len(p) != 0 {
		t.Fatalf("the default layout: %+v", p)
	}
	cases := map[string]struct {
		tiles []Tile
		code  string
	}{
		"an unknown tile": {[]Tile{{"nope", 0, 0, 4, 1}}, CodeTile},
		"a tile twice":    {[]Tile{{"cash", 0, 0, 4, 1}, {"cash", 4, 0, 4, 1}}, CodeTile},
		"past the edge":   {[]Tile{{"cash", 9, 0, 4, 1}}, CodeGrid},
		"no width":        {[]Tile{{"cash", 0, 0, 0, 1}}, CodeGrid},
		"too tall":        {[]Tile{{"cash", 0, 0, 4, 5}}, CodeGrid},
		"negative":        {[]Tile{{"cash", -1, 0, 4, 1}}, CodeGrid},
	}
	for name, c := range cases {
		found := false
		for _, p := range Validate(c.tiles) {
			found = found || p.Code == c.code
		}
		if !found {
			t.Errorf("%s: want %s", name, c.code)
		}
	}
	many := make([]Tile, 0, MaxTiles+1)
	for i := 0; i <= MaxTiles; i++ {
		many = append(many, Tile{Catalogue()[i%len(Catalogue())], 0, i, 1, 1})
	}
	if p := Validate(many); len(p) == 0 || p[0].Code != CodeTooMany {
		t.Errorf("25 tiles: %+v", p)
	}
}

package compliance

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// DataClass is a class of personal data Fyniq holds (PRD-001 CMP-PD-02, rev 0.244; p1-pdpa-retention design D3): what,
// why, where, who reads it, how long, how it is protected and where it goes abroad, and the columns that hold it.
type DataClass struct {
	Key        string   `yaml:"key"`
	What       string   `yaml:"what"`
	Why        string   `yaml:"why"`
	Where      string   `yaml:"where"`
	Who        string   `yaml:"who"`
	Retention  string   `yaml:"retention"`
	Protection string   `yaml:"protection"`
	Transfers  string   `yaml:"transfers"`
	Columns    []string `yaml:"columns"`
}

// NotPersonal is a column whose name looks personal but whose data is not, with why.
type NotPersonal struct {
	Column string `yaml:"column"`
	Why    string `yaml:"why"`
}

// NotHeld is personal data Fyniq never holds, with why.
type NotHeld struct {
	Key  string `yaml:"key"`
	What string `yaml:"what"`
	Why  string `yaml:"why"`
}

// Inventory is Fyniq's personal-data inventory, data-inventory.yaml.
type Inventory struct {
	Schema      string        `yaml:"schema"`
	Classes     []DataClass   `yaml:"classes"`
	NotPersonal []NotPersonal `yaml:"not_personal"`
	NotHeld     []NotHeld     `yaml:"not_held"`
}

// LoadInventory reads Fyniq's personal-data inventory.
func LoadInventory() (Inventory, error) {
	var inv Inventory
	b, err := files.ReadFile("data-inventory.yaml")
	if err != nil {
		return inv, err
	}
	if err := yaml.Unmarshal(b, &inv); err != nil {
		return inv, fmt.Errorf("data-inventory.yaml: %w", err)
	}
	if inv.Schema != "fyniq-data-inventory@1" {
		return inv, fmt.Errorf("data-inventory.yaml is %q, not fyniq-data-inventory@1", inv.Schema)
	}
	return inv, nil
}

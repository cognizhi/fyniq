package compliance

import "go.yaml.in/yaml/v3"

// SeedRisk is a risk of PRD-001 §19, as risks.yaml holds it for the installation's operator (CMP-06).
type SeedRisk struct {
	Ref         string `yaml:"ref"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Owner       string `yaml:"owner"`
	Likelihood  string `yaml:"likelihood"`
	Impact      string `yaml:"impact"`
	Mitigation  string `yaml:"mitigation"`
}

// Risks are the risks first-run setup loads into the organisation it creates (the owner's decision of 9 Nov 2026).
func Risks() ([]SeedRisk, error) {
	b, err := files.ReadFile("risks.yaml")
	if err != nil {
		return nil, err
	}
	var f struct {
		Risks []SeedRisk `yaml:"risks"`
	}
	err = yaml.Unmarshal(b, &f)
	return f.Risks, err
}

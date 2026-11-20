package fyniq

import "os"

func readFile(name string) (string, error) {
	b, err := os.ReadFile(name)
	return string(b), err
}

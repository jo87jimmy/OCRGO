package util

import (
	"os"

	"gopkg.in/yaml.v3"
)

var Source map[string]map[string]string

func init() {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(data, &Source)
	if err != nil {
		panic(err)
	}
}

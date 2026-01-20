package nais

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed nais.yaml
var configYaml []byte

// InboundApps is the list of allowed inbound applications from nais.yaml.
// These correspond to the accessPolicy.inbound.rules in nais.yaml.
var InboundApps []string

func init() {
	var config struct {
		Spec struct {
			AccessPolicy struct {
				Inbound struct {
					Rules []struct {
						Application string `yaml:"application"`
					} `yaml:"rules"`
				} `yaml:"inbound"`
			} `yaml:"accessPolicy"`
		} `yaml:"spec"`
	}

	if err := yaml.Unmarshal(configYaml, &config); err != nil {
		panic(fmt.Sprintf("failed to parse embedded nais.yaml: %v", err))
	}

	for _, rule := range config.Spec.AccessPolicy.Inbound.Rules {
		if rule.Application != "" {
			InboundApps = append(InboundApps, rule.Application)
		}
	}

	if len(InboundApps) == 0 {
		panic("no inbound applications found in nais.yaml")
	}
}

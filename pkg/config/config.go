package config

import (
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/pkg/types"
	"os"
)

type CNIConf struct {
	PluginConf
	SubnetConf
}

type PluginConf struct {
	types.NetConf
	RuntimeConfig *struct {
		Config map[string]interface{} `json:"config"`
	} `json:"runtimeConfig,omitempty"`

	Args *struct {
		A map[string]interface{} `json:"cni"`
	} `json:"args"`

	DataDir string `json:"dataDir"`
}

type SubnetConf struct {
	Subnet string `json:"subnet"`
	Bridge string `json:"bridge"`
}

const (
	DefaultSubnetFile = "/run/mycni/subnet.json"
	DefaultBridgeName = "cni0"
)

func LoadSubnetConfig() (*SubnetConf, error) {
	data, err := os.ReadFile(DefaultSubnetFile)
	if err != nil {
		return nil, err
	}

	conf := &SubnetConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		return nil, err
	}

	return conf, nil
}

func StoreSubnetConfig(conf *SubnetConf) error {
	data, err := json.Marshal(conf)
	if err != nil {
		return err
	}

	return os.WriteFile(DefaultSubnetFile, data, 0644)
}

func parsePluginConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	return &conf, nil
}

func LoadCNIConfig(stdin []byte) (*CNIConf, error) {
	pluginConf, err := parsePluginConfig(stdin)
	if err != nil {
		return nil, err
	}

	subnetConf, err := LoadSubnetConfig()
	if err != nil {
		return nil, err
	}
	return &CNIConf{
		PluginConf: *pluginConf,
		SubnetConf: *subnetConf,
	}, nil
}

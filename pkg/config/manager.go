package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// ManagerOpts are functional options for a Manager.
type ManagerOpts struct {
	// Fs is an abstraction for the filesystem. All filesystem operations
	// for the manager should be done through it instead of the os package.
	Fs afero.Fs

	// EnvLookup is the function used to lookup environment variables.
	// When not set it defaults to os.LookupEnv.
	EnvLookup func(key string) (string, bool)

	// Dir is the root directory for the config manager.
	Dir string
}

// Manager is able to retrieve, create, and delete configs.
type Manager struct {
	fs        afero.Fs
	envLookup func(key string) (string, bool)
	dir       string
}

// NewManager creates a new config manager.
func NewManager(opts ManagerOpts) *Manager {
	if opts.Fs == nil {
		opts.Fs = afero.NewOsFs()
	}

	if opts.EnvLookup == nil {
		opts.EnvLookup = os.LookupEnv
	}

	return &Manager{
		fs:        opts.Fs,
		dir:       opts.Dir,
		envLookup: opts.EnvLookup,
	}
}

// Current retrieves the current config.
//
// The lookup order is :
// - DCOS_CONFIG is defined and is a path to a config file.
// - DCOS_CLUSTER is defined and is the name/ID of a configured cluster.
// - An attached file exists alongside a configured cluster, OR there is a single configured cluster.
// - A legacy config file exists (at DCOS_DIR/dcos.toml).
func (m *Manager) Current() (*Config, error) {
	if configPath, ok := m.envLookup("DCOS_CONFIG"); ok {
		config := m.newConfig()
		return config, config.LoadPath(configPath)
	}

	if configName, ok := m.envLookup("DCOS_CLUSTER"); ok {
		return m.Find(configName, true)
	}

	configs := m.All()
	switch len(configs) {
	case 0:
		config := m.newConfig()
		return config, config.LoadPath(filepath.Join(m.dir, "dcos.toml"))
	case 1:
		return configs[0], nil
	default:
		var currentConfig *Config
		for _, config := range configs {
			if config.Attached() {
				if currentConfig != nil {
					return nil, errors.New("multiple clusters are attached")
				}
				currentConfig = config
			}
		}
		if currentConfig == nil {
			return nil, errors.New("no cluster is attached")
		}
		return currentConfig, nil
	}
}

// Find finds a config by cluster name or ID, `strict` indicates
// whether or not the search string can also be a cluster ID prefix.
func (m *Manager) Find(name string, strict bool) (*Config, error) {
	var matches []*Config
	for _, config := range m.All() {
		if name == config.Get(keyClusterName).(string) {
			matches = append(matches, config)
		}
		clusterID := filepath.Base(filepath.Dir(config.Path()))
		if clusterID == name {
			return config, nil
		}
		if !strict && strings.HasPrefix(clusterID, name) {
			matches = append(matches, config)
		}
	}

	switch len(matches) {
	case 0:
		return nil, errors.New("no match found")
	case 1:
		return matches[0], nil
	default:
		return nil, errors.New("multiple matches found")
	}
}

// All retrieves all configs.
func (m *Manager) All() (configs []*Config) {
	configsDir, err := m.fs.Open(filepath.Join(m.dir, "clusters"))
	if err != nil {
		return
	}
	defer configsDir.Close()

	configsDirInfo, err := configsDir.Readdir(-1)
	if err != nil {
		return
	}

	for _, configDirInfo := range configsDirInfo {
		if configDirInfo.IsDir() {
			config := m.newConfig()
			configPath := filepath.Join(configsDir.Name(), configDirInfo.Name(), "dcos.toml")
			if err := config.LoadPath(configPath); err == nil {
				configs = append(configs, config)
			}
		}
	}
	return
}

func (m *Manager) newConfig() *Config {
	return New(Opts{
		EnvLookup: m.envLookup,
		Fs:        m.fs,
	})
}
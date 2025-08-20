// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"go.yaml.in/yaml/v3"
)

// Log represents the configuration for logging.
type Log struct {
	Level    string `yaml:"level"`     // Level indicates the logging level.
	JobLevel string `yaml:"job_level"` // JobLevel indicates the job logging level.
}

// Runner represents the configuration for the runner.
type Runner struct {
	File            string            `yaml:"file"`             // File specifies the file path for the runner.
	Capacity        int               `yaml:"capacity"`         // Capacity specifies the capacity of the runner.
	Envs            map[string]string `yaml:"envs"`             // Envs stores environment variables for the runner.
	EnvFile         string            `yaml:"env_file"`         // EnvFile specifies the path to the file containing environment variables for the runner.
	Timeout         time.Duration     `yaml:"timeout"`          // Timeout specifies the duration for runner timeout.
	ShutdownTimeout time.Duration     `yaml:"shutdown_timeout"` // ShutdownTimeout specifies the duration to wait for running jobs to complete during a shutdown of the runner.
	Insecure        bool              `yaml:"insecure"`         // Insecure indicates whether the runner operates in an insecure mode.
	FetchTimeout    time.Duration     `yaml:"fetch_timeout"`    // FetchTimeout specifies the timeout duration for fetching resources.
	FetchInterval   time.Duration     `yaml:"fetch_interval"`   // FetchInterval specifies the interval duration for fetching resources.
	ReportInterval  time.Duration     `yaml:"report_interval"`  // ReportInterval specifies the interval duration for reporting status and logs of a running job.
	Labels          []string          `yaml:"labels"`           // Labels specify the labels of the runner. Labels are declared on each startup
}

// Cache represents the configuration for caching.
type Cache struct {
	Enabled                 *bool  `yaml:"enabled"`                    // Enabled indicates whether caching is enabled. It is a pointer to distinguish between false and not set. If not set, it will be true.
	Dir                     string `yaml:"dir"`                        // Dir specifies the directory path for caching.
	Host                    string `yaml:"host"`                       // Host specifies the caching host.
	Port                    uint16 `yaml:"port"`                       // Port specifies the caching port.
	ProxyPort               uint16 `yaml:"proxy_port"`                 // ProxyPort specifies the cache proxy port.
	ExternalServer          string `yaml:"external_server"`            // ExternalServer specifies the URL of external cache server
	ActionsCacheURLOverride string `yaml:"actions_cache_url_override"` // Allows the user to override the ACTIONS_CACHE_URL passed to the workflow containers
	Secret                  string `yaml:"secret"`                     // Shared secret to secure caches.
}

// Container represents the configuration for the container.
type Container struct {
	Network       string   `yaml:"network"`        // Network specifies the network for the container.
	NetworkMode   string   `yaml:"network_mode"`   // Deprecated: use Network instead. Could be removed after Gitea 1.20
	EnableIPv6    bool     `yaml:"enable_ipv6"`    // EnableIPv6 indicates whether the network is created with IPv6 enabled.
	Privileged    bool     `yaml:"privileged"`     // Privileged indicates whether the container runs in privileged mode.
	Options       string   `yaml:"options"`        // Options specifies additional options for the container.
	WorkdirParent string   `yaml:"workdir_parent"` // WorkdirParent specifies the parent directory for the container's working directory.
	ValidVolumes  []string `yaml:"valid_volumes"`  // ValidVolumes specifies the volumes (including bind mounts) can be mounted to containers.
	DockerHost    string   `yaml:"docker_host"`    // DockerHost specifies the Docker host. It overrides the value specified in environment variable DOCKER_HOST.
	ForcePull     bool     `yaml:"force_pull"`     // Pull docker image(s) even if already present
	ForceRebuild  bool     `yaml:"force_rebuild"`  // Rebuild local docker image(s) even if already present
}

// Host represents the configuration for the host.
type Host struct {
	WorkdirParent string `yaml:"workdir_parent"` // WorkdirParent specifies the parent directory for the host's working directory.
}

// Config represents the overall configuration.
type Config struct {
	Log       Log       `yaml:"log"`       // Log represents the configuration for logging.
	Runner    Runner    `yaml:"runner"`    // Runner represents the configuration for the runner.
	Cache     Cache     `yaml:"cache"`     // Cache represents the configuration for caching.
	Container Container `yaml:"container"` // Container represents the configuration for the container.
	Host      Host      `yaml:"host"`      // Host represents the configuration for the host.
}

// Tune the config settings accordingly to the Forgejo instance that will be used.
func (c *Config) Tune(instanceURL string) {
	if instanceURL == "https://codeberg.org" {
		if c.Runner.FetchInterval < 30*time.Second {
			log.Info("The runner is configured to be used by a public instance, fetch interval is set to 30 seconds.")
			c.Runner.FetchInterval = 30 * time.Second
		}
	}
}

// LoadDefault returns the default configuration.
// If file is not empty, it will be used to load the configuration.
func LoadDefault(file string) (*Config, error) {
	cfg := &Config{}
	if file != "" {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("open config file %q: %w", file, err)
		}
		if err := yaml.Unmarshal(content, cfg); err != nil {
			return nil, fmt.Errorf("parse config file %q: %w", file, err)
		}
	}
	compatibleWithOldEnvs(file != "", cfg)

	if cfg.Runner.EnvFile != "" {
		if stat, err := os.Stat(cfg.Runner.EnvFile); err == nil && !stat.IsDir() {
			envs, err := godotenv.Read(cfg.Runner.EnvFile)
			if err != nil {
				return nil, fmt.Errorf("read env file %q: %w", cfg.Runner.EnvFile, err)
			}
			if cfg.Runner.Envs == nil {
				cfg.Runner.Envs = map[string]string{}
			}
			maps.Copy(cfg.Runner.Envs, envs)
		}
	}

	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.JobLevel == "" {
		cfg.Log.JobLevel = "info"
	}
	if cfg.Runner.File == "" {
		cfg.Runner.File = ".runner"
	}
	if cfg.Runner.Capacity <= 0 {
		cfg.Runner.Capacity = 1
	}
	if cfg.Runner.Timeout <= 0 {
		cfg.Runner.Timeout = 3 * time.Hour
	}
	if cfg.Cache.Enabled == nil {
		b := true
		cfg.Cache.Enabled = &b
	}
	if *cfg.Cache.Enabled {
		if cfg.Cache.Dir == "" {
			home, _ := os.UserHomeDir()
			cfg.Cache.Dir = filepath.Join(home, ".cache", "actcache")
		}
	}
	if cfg.Container.WorkdirParent == "" {
		cfg.Container.WorkdirParent = "workspace"
	}
	if cfg.Host.WorkdirParent == "" {
		home, _ := os.UserHomeDir()
		cfg.Host.WorkdirParent = filepath.Join(home, ".cache", "act")
	}
	if cfg.Runner.FetchTimeout <= 0 {
		cfg.Runner.FetchTimeout = 5 * time.Second
	}
	if cfg.Runner.FetchInterval <= 0 {
		cfg.Runner.FetchInterval = 2 * time.Second
	}
	if cfg.Runner.ReportInterval <= 0 {
		cfg.Runner.ReportInterval = time.Second
	}

	if cfg.Container.DockerHost == "" {
		cfg.Container.DockerHost = "-"
	}

	// although `container.network_mode` will be deprecated, but we have to be compatible with it for now.
	if cfg.Container.NetworkMode != "" && cfg.Container.Network == "" {
		log.Warn("You are trying to use deprecated configuration item of `container.network_mode`, please use `container.network` instead.")
		if cfg.Container.NetworkMode == "bridge" {
			// Previously, if the value of `container.network_mode` is `bridge`, we will create a new network for job.
			// But “bridge” is easily confused with the bridge network created by Docker by default.
			// So we set the value of `container.network` to empty string to make `act_runner` automatically create a new network for job.
			cfg.Container.Network = ""
		} else {
			cfg.Container.Network = cfg.Container.NetworkMode
		}
	}

	return cfg, nil
}

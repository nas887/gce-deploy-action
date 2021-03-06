package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"gopkg.in/yaml.v2"
)

var environ = os.Environ()

type GithubActionConfig struct {
	Config                           string
	GoogleApplicationCredentials     string
	googleApplicationCredentialsData string
}

func ReadGithubActionConfig() (*GithubActionConfig, error) {
	c := &GithubActionConfig{}

	c.Config = os.Getenv("INPUT_CONFIG")
	if c.Config == "" {
		c.Config = "deploy.yml"
	}

	// read Google Application Credentials if this is a path
	c.GoogleApplicationCredentials = os.Getenv("INPUT_CREDS")
	f, err := ioutil.ReadFile(c.GoogleApplicationCredentials)
	if err == nil {
		c.googleApplicationCredentialsData = string(f)
	} else {
		c.googleApplicationCredentialsData = c.GoogleApplicationCredentials
	}

	return c, nil
}

func ReadConfigFile(path string) (io.ReadCloser, error) {
	paths := []string{path}

	switch filepath.Ext(path) {
	case "yml":
		paths = append(paths, path[:len(path)-4]+".yaml")
	case "yaml":
		paths = append(paths, path[:len(path)-5]+".yml")
	}

	for _, p := range paths {
		f, err := os.Open(p)
		if err == nil {
			return f, nil
		}
	}

	return nil, fmt.Errorf("config: %v", path)
}

type Config struct {
	DeleteInstanceTemplatesAfter string `yaml:"delete_instance_templates_after"`
	deleteInstanceTemplatesAfter time.Duration
	Common                       Common   `yaml:"common"`
	Deploys                      []Deploy `yaml:"deploys"`
}

type Common struct {
	Project            string            `yaml:"project"`
	Region             string            `yaml:"region"`
	StartupScriptPath  string            `yaml:"startup_script"`
	ShutdownScriptPath string            `yaml:"shutdown_script"`
	CloudInitPath      string            `yaml:"cloud_init"`
	Vars               map[string]string `yaml:"vars"`
	Labels             map[string]string `yaml:"labels"`
	Metadata           map[string]string `yaml:"metadata"`
	Tags               []string          `yaml:"tags"`
	UpdatePolicy       UpdatePolicy      `yaml:"update_policy"`
}

type Deploy struct {
	Name                             string `yaml:"name"`
	Project                          string `yaml:"project"`
	GoogleApplicationCredentials     string `yaml:"creds"`
	googleApplicationCredentialsData string
	Region                           string `yaml:"region"`
	InstanceGroup                    string `yaml:"instance_group"`
	InstanceTemplateBase             string `yaml:"instance_template_base"`
	InstanceTemplate                 string `yaml:"instance_template"`
	StartupScriptPath                string `yaml:"startup_script"`
	startupScript                    string
	ShutdownScriptPath               string `yaml:"shutdown_script"`
	shutdownScript                   string
	CloudInitPath                    string `yaml:"cloud_init"`
	cloudInit                        string
	Vars                             map[string]string `yaml:"vars"`
	Labels                           map[string]string `yaml:"labels"`
	Metadata                         map[string]string `yaml:"metadata"`
	Tags                             []string          `yaml:"tags"`
	UpdatePolicy                     UpdatePolicy      `yaml:"update_policy"`
}

type UpdatePolicy struct {
	Type                    string `yaml:"type"`
	ReplacementMethod       string `yaml:"replacement_method"`
	MinimalAction           string `yaml:"minimal_action"`
	MinReadySec             string `yaml:"min_ready_sec"`
	minReadySec             int
	MaxSurge                string `yaml:"max_surge"`
	maxSurge                int
	maxSurgeInPercent       bool
	MaxUnavailable          string `yaml:"max_unavailable"`
	maxUnavailable          int
	maxUnavailableInPercent bool
}

func ParseConfig(b io.Reader) (*Config, error) {
	c := &Config{}
	d := yaml.NewDecoder(b)
	d.SetStrict(true)
	if err := d.Decode(c); err != nil && err != io.EOF {
		return nil, fmt.Errorf("config: %v", err)
	}

	// merge common config
	for i := 0; i < len(c.Deploys); i++ {
		deploy := &c.Deploys[i]

		if strings.TrimSpace(deploy.Project) == "" {
			deploy.Project = c.Common.Project
		}
		if strings.TrimSpace(deploy.Region) == "" {
			deploy.Region = c.Common.Region
		}
		if strings.TrimSpace(deploy.StartupScriptPath) == "" {
			deploy.StartupScriptPath = c.Common.StartupScriptPath
		}
		if strings.TrimSpace(deploy.ShutdownScriptPath) == "" {
			deploy.ShutdownScriptPath = c.Common.ShutdownScriptPath
		}
		if strings.TrimSpace(deploy.CloudInitPath) == "" {
			deploy.CloudInitPath = c.Common.CloudInitPath
		}

		if deploy.Vars == nil {
			deploy.Vars = make(map[string]string)
		}
		for k, v := range c.Common.Vars {
			if _, ok := deploy.Vars[k]; !ok {
				deploy.Vars[k] = v
			}
		}

		if deploy.Labels == nil {
			deploy.Labels = make(map[string]string)
		}
		for k, v := range c.Common.Labels {
			if _, ok := deploy.Labels[k]; !ok {
				deploy.Labels[k] = v
			}
		}

		if deploy.Metadata == nil {
			deploy.Metadata = make(map[string]string)
		}
		for k, v := range c.Common.Metadata {
			if _, ok := deploy.Metadata[k]; !ok {
				deploy.Metadata[k] = v
			}
		}

		deploy.Tags = append(deploy.Tags, c.Common.Tags...)

		if strings.TrimSpace(deploy.UpdatePolicy.Type) == "" {
			deploy.UpdatePolicy.Type = c.Common.UpdatePolicy.Type
		}
		if strings.TrimSpace(deploy.UpdatePolicy.ReplacementMethod) == "" {
			deploy.UpdatePolicy.ReplacementMethod = c.Common.UpdatePolicy.ReplacementMethod
		}
		if strings.TrimSpace(deploy.UpdatePolicy.MinimalAction) == "" {
			deploy.UpdatePolicy.MinimalAction = c.Common.UpdatePolicy.MinimalAction
		}
		if strings.TrimSpace(deploy.UpdatePolicy.MinimalAction) == "" {
			deploy.UpdatePolicy.MinimalAction = c.Common.UpdatePolicy.MinimalAction
		}
		if strings.TrimSpace(deploy.UpdatePolicy.MinReadySec) == "" {
			deploy.UpdatePolicy.MinReadySec = c.Common.UpdatePolicy.MinReadySec
		}
		if strings.TrimSpace(deploy.UpdatePolicy.MaxSurge) == "" {
			deploy.UpdatePolicy.MaxSurge = c.Common.UpdatePolicy.MaxSurge
		}
		if strings.TrimSpace(deploy.UpdatePolicy.MaxUnavailable) == "" {
			deploy.UpdatePolicy.MaxUnavailable = c.Common.UpdatePolicy.MaxUnavailable
		}
	}

	// if DeleteInstanceTemplatesAfter is not set to false
	if c.DeleteInstanceTemplatesAfter != "false" {
		// parse and set duration if set
		if c.DeleteInstanceTemplatesAfter != "" {
			duration, err := time.ParseDuration(c.DeleteInstanceTemplatesAfter)
			if err != nil {
				return nil, err
			}
			c.deleteInstanceTemplatesAfter = duration
		} else {
			// or set default
			c.deleteInstanceTemplatesAfter = 24 * time.Hour * 14 // 14 days
		}
	}

	// expand env variables
	for i := range c.Deploys {
		dy := &c.Deploys[i]

		dy.Name = expandVars(dy.Name, getEnv(nil))
		if dy.Name == "" {
			return nil, fmt.Errorf("deploy item #%v needs name", i+1)
		}

		dy.Project = expandVars(dy.Project, getEnv(nil))

		dy.GoogleApplicationCredentials = expandVars(dy.GoogleApplicationCredentials, getEnv(nil))

		f, err := ioutil.ReadFile(dy.GoogleApplicationCredentials)
		if err == nil {
			dy.googleApplicationCredentialsData = string(f)
		} else {
			dy.googleApplicationCredentialsData = dy.GoogleApplicationCredentials
		}

		dy.Region = expandVars(dy.Region, getEnv(nil))
		if dy.Region == "" {
			return nil, fmt.Errorf("deploy '%v' needs region", dy.Name)
		}

		dy.InstanceGroup = expandVars(dy.InstanceGroup, getEnv(nil))
		if dy.InstanceGroup == "" {
			return nil, fmt.Errorf("deploy '%v' needs instance_group", dy.Name)
		}

		dy.InstanceTemplateBase = expandVars(dy.InstanceTemplateBase, getEnv(nil))
		if dy.InstanceTemplateBase == "" {
			return nil, fmt.Errorf("deploy '%v' needs instance_template_base", dy.Name)
		}

		dy.InstanceTemplate = expandVars(dy.InstanceTemplate, getEnv(nil))
		if dy.InstanceTemplate == "" {
			return nil, fmt.Errorf("deploy '%v' needs instance_template", dy.Name)
		}

		dy.StartupScriptPath = expandVars(dy.StartupScriptPath, getEnv(nil))

		dy.ShutdownScriptPath = expandVars(dy.ShutdownScriptPath, getEnv(nil))

		dy.CloudInitPath = expandVars(dy.CloudInitPath, getEnv(nil))

		for k, v := range dy.Vars {
			dy.Vars[k] = expandVars(v, getEnv(nil))
		}

		for k, v := range dy.Labels {
			dy.Labels[k] = expandVars(v, getEnv(nil))
		}

		for k, v := range dy.Metadata {
			dy.Metadata[k] = expandVars(v, getEnv(nil))
		}

		for j := range dy.Tags {
			dy.Tags[j] = expandVars(dy.Tags[j], getEnv(nil))
		}

		// expand vars in update policy
		dy.UpdatePolicy.Type = expandVars(dy.UpdatePolicy.Type, getEnv(nil))
		dy.UpdatePolicy.MinimalAction = expandVars(dy.UpdatePolicy.MinimalAction, getEnv(nil))
		dy.UpdatePolicy.ReplacementMethod = expandVars(dy.UpdatePolicy.ReplacementMethod, getEnv(nil))
		dy.UpdatePolicy.MinReadySec = expandVars(dy.UpdatePolicy.MinReadySec, getEnv(nil))
		dy.UpdatePolicy.MaxSurge = expandVars(dy.UpdatePolicy.MaxSurge, getEnv(nil))
		dy.UpdatePolicy.MaxUnavailable = expandVars(dy.UpdatePolicy.MaxUnavailable, getEnv(nil))

		if strings.TrimSpace(dy.UpdatePolicy.Type) == "" {
			dy.UpdatePolicy.Type = "PROACTIVE"
		}

		if strings.TrimSpace(dy.UpdatePolicy.MinimalAction) == "" {
			dy.UpdatePolicy.MinimalAction = "REPLACE"
		}

		if strings.TrimSpace(dy.UpdatePolicy.ReplacementMethod) == "" {
			dy.UpdatePolicy.ReplacementMethod = "SUBSTITUTE"
		}

		// parse update policy vars
		if dy.UpdatePolicy.MinReadySec != "" {
			minReadySec, err := strconv.Atoi(dy.UpdatePolicy.MinReadySec)
			if err != nil {
				return nil, fmt.Errorf("update_policy.min_ready_sec: %v", err)
			}
			dy.UpdatePolicy.minReadySec = minReadySec
		} else {
			dy.UpdatePolicy.minReadySec = 10 // set default
		}

		if dy.UpdatePolicy.MaxSurge != "" {
			dy.UpdatePolicy.MaxSurge = strings.TrimSpace(dy.UpdatePolicy.MaxSurge)
			if strings.HasSuffix(dy.UpdatePolicy.MaxSurge, "%") {
				maxSurge, err := strconv.Atoi(strings.TrimSuffix(dy.UpdatePolicy.MaxSurge, "%"))
				if err != nil {
					return nil, fmt.Errorf("update_policy.max_surge: %v", err)
				}
				dy.UpdatePolicy.maxSurge = maxSurge
				dy.UpdatePolicy.maxSurgeInPercent = true
			} else {
				maxSurge, err := strconv.Atoi(dy.UpdatePolicy.MaxSurge)
				if err != nil {
					return nil, fmt.Errorf("update_policy.max_surge: %v", err)
				}
				dy.UpdatePolicy.maxSurge = maxSurge
			}
		} else {
			dy.UpdatePolicy.maxSurge = 3 // set default
		}

		if dy.UpdatePolicy.MaxUnavailable != "" {
			dy.UpdatePolicy.MaxUnavailable = strings.TrimSpace(dy.UpdatePolicy.MaxUnavailable)
			if strings.HasSuffix(dy.UpdatePolicy.MaxUnavailable, "%") {
				maxUnavailable, err := strconv.Atoi(strings.TrimSuffix(dy.UpdatePolicy.MaxUnavailable, "%"))
				if err != nil {
					return nil, fmt.Errorf("update_policy.max_unavailable: %v", err)
				}
				dy.UpdatePolicy.maxUnavailable = maxUnavailable
				dy.UpdatePolicy.maxUnavailableInPercent = true
			} else {
				maxUnavailable, err := strconv.Atoi(dy.UpdatePolicy.MaxUnavailable)
				if err != nil {
					return nil, fmt.Errorf("update_policy.max_unavailable: %v", err)
				}
				dy.UpdatePolicy.maxUnavailable = maxUnavailable
			}
		} else {
			dy.UpdatePolicy.maxUnavailable = 0 // set default
		}
	}

	// read contents of scripts and expand env vars
	for i := range c.Deploys {
		dy := &c.Deploys[i]

		if dy.StartupScriptPath != "" {
			f, err := downloadOrReadFile(dy.StartupScriptPath)
			if err != nil {
				return nil, fmt.Errorf("startup_script: %v", err)
			}
			dy.startupScript = expandVars(string(f), getEnv(dy.Vars))
		}

		if dy.ShutdownScriptPath != "" {
			f, err := downloadOrReadFile(dy.ShutdownScriptPath)
			if err != nil {
				return nil, fmt.Errorf("shutdown_script: %v", err)
			}
			dy.shutdownScript = expandVars(string(f), getEnv(dy.Vars))
		}

		if dy.CloudInitPath != "" {
			f, err := downloadOrReadFile(dy.CloudInitPath)
			if err != nil {
				return nil, fmt.Errorf("cloud_init: %v", err)
			}
			dy.cloudInit = expandVars(string(f), getEnv(dy.Vars))
		}
	}

	return c, nil
}

func getEnv(locals map[string]string) map[string]string {
	m := make(map[string]string)

	for _, v := range environ {
		x := strings.SplitN(v, "=", 2)
		m[strings.ToLower(x[0])] = x[1]
	}

	for k, v := range locals {
		m[strings.ToLower(k)] = v
	}

	return m
}

var (
	variableRe = regexp.MustCompile(`\\?\$\{\{ *([a-zA-Z]([a-zA-Z0-9-_]+[a-zA-Z0-9]|[a-zA-Z0-9]*)(:\d(:\d)?)?) *\}\}`)
)

// expandVars replaces ${{VAR}}
func expandVars(str string, vars map[string]string) string {
	return variableRe.ReplaceAllStringFunc(str, func(x string) string {
		if strings.HasPrefix(x, `\$`) {
			return x
		}

		x = strings.Trim(x, "${}")
		x = strings.TrimSpace(x)

		if !strings.Contains(x, ":") {
			return vars[strings.ToLower(x)]
		}

		// parse ${{string:position[:length]}} and truncate string
		parts := strings.Split(x, ":")
		switch len(parts) {
		default:
			fallthrough
		case 1:
			return vars[strings.ToLower(parts[0])]

		case 2:
			v := vars[strings.ToLower(parts[0])]

			from, err := strconv.Atoi(parts[1])
			if err != nil {
				return v
			}
			return v[from:]

		case 3:
			v := vars[strings.ToLower(parts[0])]

			from, err := strconv.Atoi(parts[1])
			if err != nil {
				return v
			}

			to, err := strconv.Atoi(parts[2])
			if err != nil {
				return v
			}
			return v[from : from+to]
		}
	})
}

func downloadOrReadFile(path string) ([]byte, error) {
	path = strings.TrimSpace(path)

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		client := retryablehttp.NewClient()
		client.RetryMax = 3
		client.RetryWaitMax = 5 * time.Second

		resp, err := client.Get(path)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return ioutil.ReadAll(resp.Body)

	} else {
		return ioutil.ReadFile(path)
	}
}

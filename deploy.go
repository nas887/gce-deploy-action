package main

import (
	"net/http"

	computeBeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
)

func Run(githubActionConfig *GithubActionConfig, config *Config, deploy Deploy) error {

	// create google client with application credentials from deploy config or
	// github action config
	var googleClient *http.Client
	if deploy.googleApplicationCredentialsData != "" {
		client, f, err := NewClientFromJSON(deploy.googleApplicationCredentialsData)
		if err != nil {
			Fatalf("Invalid deploys.*.creds: %v", err)
		}
		googleClient = client

		if deploy.Project == "" {
			deploy.Project = f.ProjectID
		}

	} else {
		client, f, err := NewClientFromJSON(githubActionConfig.googleApplicationCredentialsData)
		if err != nil {
			Fatalf("Invalid github_action.creds: %v", err)
		}
		googleClient = client

		if deploy.Project == "" {
			deploy.Project = f.ProjectID
		}
	}

	// create compute service client
	computeService, err := compute.New(googleClient)
	if err != nil {
		return err
	}

	// create compute beta service client
	computeBetaService, err := computeBeta.New(googleClient)
	if err != nil {
		return err
	}

	// clone instance template and update instance group
	instanceTemplateURL, err := CloneInstanceTemplate(computeService, deploy)
	if err != nil {
		return err
	}

	Infof("%v: Created new instance template '%v/%v'", deploy.Name, deploy.Project, deploy.InstanceTemplate)

	// start rolling update via instance group manager
	if err := StartRollingUpdate(computeBetaService, deploy, instanceTemplateURL); err != nil {
		return err
	}

	Infof("%v: Started rolling deploy for instance group '%v/%v'", deploy.Name, deploy.Project, deploy.InstanceGroup)

	if config.DeleteInstanceTemplatesAfter > 0 {
		if err := CleanupInstanceTemplates(computeService, deploy.Project, config.DeleteInstanceTemplatesAfter); err != nil {
			LogWarning(err.Error(), map[string]string{"project": deploy.Project})
		}
	}

	return nil
}

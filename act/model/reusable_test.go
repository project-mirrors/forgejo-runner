package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRemoteReusableWorkflowWithPlat(t *testing.T) {
	tests := []struct {
		name             string
		url              string
		uses             string
		expectedOrg      string
		expectedRepo     string
		expectedPlatform string
		expectedFilename string
		expectedRef      string
		shouldFail       bool
	}{
		{
			name:             "valid github workflow",
			url:              "github.com",
			uses:             "owner/repo/.github/workflows/test.yml@main",
			expectedOrg:      "owner",
			expectedRepo:     "repo",
			expectedPlatform: "github",
			expectedFilename: "test.yml",
			expectedRef:      "main",
			shouldFail:       false,
		},
		{
			name:             "valid gitea workflow",
			url:              "code.forgejo.org",
			uses:             "forgejo/runner/.gitea/workflows/build.yaml@v1.0.0",
			expectedOrg:      "forgejo",
			expectedRepo:     "runner",
			expectedPlatform: "gitea",
			expectedFilename: "build.yaml",
			expectedRef:      "v1.0.0",
			shouldFail:       false,
		},
		{
			name:       "invalid format - missing platform",
			url:        "github.com",
			uses:       "owner/repo/workflows/test.yml@main",
			shouldFail: true,
		},
		{
			name:       "invalid format - no ref",
			url:        "github.com",
			uses:       "owner/repo/.github/workflows/test.yml",
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewRemoteReusableWorkflowWithPlat(tt.url, tt.uses)

			if tt.shouldFail {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedOrg, result.Org)
				assert.Equal(t, tt.expectedRepo, result.Repo)
				assert.Equal(t, tt.expectedPlatform, result.GitPlatform)
				assert.Equal(t, tt.expectedFilename, result.Filename)
				assert.Equal(t, tt.expectedRef, result.Ref)
				assert.Equal(t, tt.url, result.URL)
			}
		})
	}
}

func TestRemoteReusableWorkflow_CloneURL(t *testing.T) {
	t.Run("adds https prefix when missing", func(t *testing.T) {
		rw := &RemoteReusableWorkflow{
			URL:  "code.forgejo.org",
			Org:  "owner",
			Repo: "repo",
		}
		assert.Equal(t, "https://code.forgejo.org/owner/repo", rw.CloneURL())
	})

	t.Run("preserves https prefix", func(t *testing.T) {
		rw := &RemoteReusableWorkflow{
			URL:  "https://code.forgejo.org",
			Org:  "owner",
			Repo: "repo",
		}
		assert.Equal(t, "https://code.forgejo.org/owner/repo", rw.CloneURL())
	})

	t.Run("preserves http prefix", func(t *testing.T) {
		rw := &RemoteReusableWorkflow{
			URL:  "http://localhost:3000",
			Org:  "owner",
			Repo: "repo",
		}
		assert.Equal(t, "http://localhost:3000/owner/repo", rw.CloneURL())
	})
}

func TestRemoteReusableWorkflow_FilePath(t *testing.T) {
	tests := []struct {
		name         string
		gitPlatform  string
		filename     string
		expectedPath string
	}{
		{
			name:         "github platform",
			gitPlatform:  "github",
			filename:     "test.yml",
			expectedPath: "./.github/workflows/test.yml",
		},
		{
			name:         "gitea platform",
			gitPlatform:  "gitea",
			filename:     "build.yaml",
			expectedPath: "./.gitea/workflows/build.yaml",
		},
		{
			name:         "forgejo platform",
			gitPlatform:  "forgejo",
			filename:     "deploy.yml",
			expectedPath: "./.forgejo/workflows/deploy.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rw := &RemoteReusableWorkflow{
				GitPlatform: tt.gitPlatform,
				Filename:    tt.filename,
			}
			assert.Equal(t, tt.expectedPath, rw.FilePath())
		})
	}
}

package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"code.forgejo.org/forgejo/runner/v9/act/common"
)

func TestFindGitSlug(t *testing.T) {
	assert := assert.New(t)

	slugTests := []struct {
		url      string // input
		provider string // expected result
		slug     string // expected result
	}{
		{"https://git-codecommit.us-east-1.amazonaws.com/v1/repos/my-repo-name", "CodeCommit", "my-repo-name"},
		{"ssh://git-codecommit.us-west-2.amazonaws.com/v1/repos/my-repo", "CodeCommit", "my-repo"},
		{"git@github.com:nektos/act.git", "GitHub", "nektos/act"},
		{"git@github.com:nektos/act", "GitHub", "nektos/act"},
		{"https://github.com/nektos/act.git", "GitHub", "nektos/act"},
		{"http://github.com/nektos/act.git", "GitHub", "nektos/act"},
		{"https://github.com/nektos/act", "GitHub", "nektos/act"},
		{"http://github.com/nektos/act", "GitHub", "nektos/act"},
		{"git+ssh://git@github.com/owner/repo.git", "GitHub", "owner/repo"},
		{"http://myotherrepo.com/act.git", "", "http://myotherrepo.com/act.git"},
		{"ssh://git@example.com/forgejo/act.git", "GitHubEnterprise", "forgejo/act"},
		{"ssh://git@example.com:2222/forgejo/act.git", "GitHubEnterprise", "forgejo/act"},
		{"ssh://git@example.com:forgejo/act.git", "GitHubEnterprise", "forgejo/act"},
		{"ssh://git@example.com:/forgejo/act.git", "GitHubEnterprise", "forgejo/act"},
	}

	for _, tt := range slugTests {
		instance := "example.com"
		if tt.provider == "GitHub" {
			instance = "github.com"
		}

		provider, slug, err := findGitSlug(tt.url, instance)

		assert.NoError(err)
		assert.Equal(tt.provider, provider)
		assert.Equal(tt.slug, slug)
	}
}

func cleanGitHooks(dir string) error {
	hooksDir := filepath.Join(dir, ".git", "hooks")
	files, err := os.ReadDir(hooksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		relName := filepath.Join(hooksDir, f.Name())
		if err := os.Remove(relName); err != nil {
			return err
		}
	}
	return nil
}

func TestFindGitRemoteURL(t *testing.T) {
	assert := assert.New(t)

	basedir := t.TempDir()
	gitConfig()
	err := gitCmd("init", basedir)
	assert.NoError(err)
	err = cleanGitHooks(basedir)
	assert.NoError(err)

	remoteURL := "https://git-codecommit.us-east-1.amazonaws.com/v1/repos/my-repo-name"
	err = gitCmd("-C", basedir, "remote", "add", "origin", remoteURL)
	assert.NoError(err)

	u, err := findGitRemoteURL(t.Context(), basedir, "origin")
	assert.NoError(err)
	assert.Equal(remoteURL, u)

	remoteURL = "git@github.com/AwesomeOwner/MyAwesomeRepo.git"
	err = gitCmd("-C", basedir, "remote", "add", "upstream", remoteURL)
	assert.NoError(err)
	u, err = findGitRemoteURL(t.Context(), basedir, "upstream")
	assert.NoError(err)
	assert.Equal(remoteURL, u)
}

func TestGitFindRef(t *testing.T) {
	basedir := t.TempDir()
	gitConfig()

	for name, tt := range map[string]struct {
		Prepare func(t *testing.T, dir string)
		Assert  func(t *testing.T, ref string, err error)
	}{
		"new_repo": {
			Prepare: func(t *testing.T, dir string) {},
			Assert: func(t *testing.T, ref string, err error) {
				require.Error(t, err)
			},
		},
		"new_repo_with_commit": {
			Prepare: func(t *testing.T, dir string) {
				require.NoError(t, gitCmd("-C", dir, "commit", "--allow-empty", "-m", "msg"))
			},
			Assert: func(t *testing.T, ref string, err error) {
				require.NoError(t, err)
				require.Equal(t, "refs/heads/master", ref)
			},
		},
		"current_head_is_tag": {
			Prepare: func(t *testing.T, dir string) {
				require.NoError(t, gitCmd("-C", dir, "commit", "--allow-empty", "-m", "commit msg"))
				require.NoError(t, gitCmd("-C", dir, "tag", "v1.2.3"))
				require.NoError(t, gitCmd("-C", dir, "checkout", "v1.2.3"))
			},
			Assert: func(t *testing.T, ref string, err error) {
				require.NoError(t, err)
				require.Equal(t, "refs/tags/v1.2.3", ref)
			},
		},
		"current_head_is_same_as_tag": {
			Prepare: func(t *testing.T, dir string) {
				require.NoError(t, gitCmd("-C", dir, "commit", "--allow-empty", "-m", "1.4.2 release"))
				require.NoError(t, gitCmd("-C", dir, "tag", "v1.4.2"))
			},
			Assert: func(t *testing.T, ref string, err error) {
				require.NoError(t, err)
				require.Equal(t, "refs/tags/v1.4.2", ref)
			},
		},
		"current_head_is_not_tag": {
			Prepare: func(t *testing.T, dir string) {
				require.NoError(t, gitCmd("-C", dir, "commit", "--allow-empty", "-m", "msg"))
				require.NoError(t, gitCmd("-C", dir, "tag", "v1.4.2"))
				require.NoError(t, gitCmd("-C", dir, "commit", "--allow-empty", "-m", "msg2"))
			},
			Assert: func(t *testing.T, ref string, err error) {
				require.NoError(t, err)
				require.Equal(t, "refs/heads/master", ref)
			},
		},
		"current_head_is_another_branch": {
			Prepare: func(t *testing.T, dir string) {
				require.NoError(t, gitCmd("-C", dir, "checkout", "-b", "mybranch"))
				require.NoError(t, gitCmd("-C", dir, "commit", "--allow-empty", "-m", "msg"))
			},
			Assert: func(t *testing.T, ref string, err error) {
				require.NoError(t, err)
				require.Equal(t, "refs/heads/mybranch", ref)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(basedir, name)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, gitCmd("-C", dir, "init", "--initial-branch=master"))
			require.NoError(t, gitCmd("-C", dir, "config", "user.name", "user@example.com"))
			require.NoError(t, gitCmd("-C", dir, "config", "user.email", "user@example.com"))
			require.NoError(t, cleanGitHooks(dir))
			tt.Prepare(t, dir)
			ref, err := FindGitRef(t.Context(), dir)
			tt.Assert(t, ref, err)
		})
	}
}

func TestGitCloneExecutor(t *testing.T) {
	for name, tt := range map[string]struct {
		Err      error
		URL, Ref string
	}{
		"tag": {
			Err: nil,
			URL: "https://github.com/actions/checkout",
			Ref: "v2",
		},
		"branch": {
			Err: nil,
			URL: "https://github.com/anchore/scan-action",
			Ref: "act-fails",
		},
		"sha": {
			Err: nil,
			URL: "https://github.com/actions/checkout",
			Ref: "5a4ac9002d0be2fb38bd78e4b4dbde5606d7042f", // v2
		},
		"short-sha": {
			Err: &Error{ErrShortRef, "5a4ac9002d0be2fb38bd78e4b4dbde5606d7042f"},
			URL: "https://github.com/actions/checkout",
			Ref: "5a4ac90", // v2
		},
	} {
		t.Run(name, func(t *testing.T) {
			clone := NewGitCloneExecutor(NewGitCloneExecutorInput{
				URL: tt.URL,
				Ref: tt.Ref,
				Dir: t.TempDir(),
			})

			err := clone(t.Context())
			if tt.Err != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.Err, err)
			} else {
				assert.Empty(t, err)
			}
		})
	}
}

func gitConfig() {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		var err error
		if err = gitCmd("config", "--global", "user.email", "test@test.com"); err != nil {
			log.Error(err)
		}
		if err = gitCmd("config", "--global", "user.name", "Unit Test"); err != nil {
			log.Error(err)
		}
	}
}

func gitCmd(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if exitError, ok := err.(*exec.ExitError); ok {
		if waitStatus, ok := exitError.Sys().(syscall.WaitStatus); ok {
			return fmt.Errorf("Exit error %d", waitStatus.ExitStatus())
		}
		return exitError
	}
	return nil
}

func TestCloneIfRequired(t *testing.T) {
	tempDir := t.TempDir()
	ctx := t.Context()

	t.Run("clone", func(t *testing.T) {
		repo, err := CloneIfRequired(ctx, "refs/heads/main", NewGitCloneExecutorInput{
			URL: "https://github.com/actions/checkout",
			Dir: tempDir,
		}, common.Logger(ctx))
		assert.NoError(t, err)
		assert.NotNil(t, repo)
	})

	t.Run("clone different remote", func(t *testing.T) {
		repo, err := CloneIfRequired(ctx, "refs/heads/main", NewGitCloneExecutorInput{
			URL: "https://github.com/actions/setup-go",
			Dir: tempDir,
		}, common.Logger(ctx))
		require.NoError(t, err)
		require.NotNil(t, repo)

		remote, err := repo.Remote("origin")
		require.NoError(t, err)
		require.Len(t, remote.Config().URLs, 1)
		assert.Equal(t, "https://github.com/actions/setup-go", remote.Config().URLs[0])
	})
}

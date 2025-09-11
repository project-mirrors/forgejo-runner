package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"code.forgejo.org/forgejo/runner/v11/act/common"
	"code.forgejo.org/forgejo/runner/v11/act/model"
)

var (
	baseImage = "code.forgejo.org/oci/node:20-bookworm"
	platforms map[string]string
	logLevel  = log.DebugLevel
	workdir   = "testdata"
	secrets   map[string]string
)

func init() {
	if p := os.Getenv("ACT_TEST_IMAGE"); p != "" {
		baseImage = p
	}

	platforms = map[string]string{
		"ubuntu-latest": baseImage,
	}

	if l := os.Getenv("ACT_TEST_LOG_LEVEL"); l != "" {
		if lvl, err := log.ParseLevel(l); err == nil {
			logLevel = lvl
		}
	}

	if wd, err := filepath.Abs(workdir); err == nil {
		workdir = wd
	}

	secrets = map[string]string{}
}

func TestRunner_NoWorkflowsFoundByPlanner(t *testing.T) {
	planner, err := model.NewWorkflowPlanner("res", true, false)
	assert.NoError(t, err)

	out := log.StandardLogger().Out
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.DebugLevel)
	plan, err := planner.PlanEvent("pull_request")
	assert.NotNil(t, plan)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "no workflows found by planner")
	buf.Reset()
	plan, err = planner.PlanAll()
	assert.NotNil(t, plan)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "no workflows found by planner")
	log.SetOutput(out)
}

func TestRunner_GraphMissingEvent(t *testing.T) {
	planner, err := model.NewWorkflowPlanner("testdata/issue-1595/no-event.yml", true, false)
	assert.NoError(t, err)

	out := log.StandardLogger().Out
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.DebugLevel)

	plan, err := planner.PlanEvent("push")
	assert.NoError(t, err)
	assert.NotNil(t, plan)
	assert.Equal(t, 0, len(plan.Stages))

	assert.Contains(t, buf.String(), "no events found for workflow: no-event.yml")
	log.SetOutput(out)
}

func TestRunner_GraphMissingFirst(t *testing.T) {
	planner, err := model.NewWorkflowPlanner("testdata/issue-1595/no-first.yml", true, false)
	assert.NoError(t, err)

	plan, err := planner.PlanEvent("push")
	assert.EqualError(t, err, "unable to build dependency graph for no first (no-first.yml)")
	assert.NotNil(t, plan)
	assert.Equal(t, 0, len(plan.Stages))
}

func TestRunner_GraphWithMissing(t *testing.T) {
	planner, err := model.NewWorkflowPlanner("testdata/issue-1595/missing.yml", true, false)
	assert.NoError(t, err)

	out := log.StandardLogger().Out
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.DebugLevel)

	plan, err := planner.PlanEvent("push")
	assert.NotNil(t, plan)
	assert.Equal(t, 0, len(plan.Stages))
	assert.EqualError(t, err, "unable to build dependency graph for missing (missing.yml)")
	assert.Contains(t, buf.String(), "unable to build dependency graph for missing (missing.yml)")
	log.SetOutput(out)
}

func TestRunner_GraphWithSomeMissing(t *testing.T) {
	log.SetLevel(log.DebugLevel)

	planner, err := model.NewWorkflowPlanner("testdata/issue-1595/", true, false)
	assert.NoError(t, err)

	out := log.StandardLogger().Out
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(log.DebugLevel)

	plan, err := planner.PlanAll()
	assert.Error(t, err, "unable to build dependency graph for no first (no-first.yml)")
	assert.NotNil(t, plan)
	assert.Equal(t, 1, len(plan.Stages))
	assert.Contains(t, buf.String(), "unable to build dependency graph for missing (missing.yml)")
	assert.Contains(t, buf.String(), "unable to build dependency graph for no first (no-first.yml)")
	log.SetOutput(out)
}

func TestRunner_GraphEvent(t *testing.T) {
	planner, err := model.NewWorkflowPlanner("testdata/basic", true, false)
	assert.NoError(t, err)

	plan, err := planner.PlanEvent("push")
	assert.NoError(t, err)
	assert.NotNil(t, plan)
	assert.NotNil(t, plan.Stages)
	assert.Equal(t, len(plan.Stages), 3, "stages")
	assert.Equal(t, len(plan.Stages[0].Runs), 1, "stage0.runs")
	assert.Equal(t, len(plan.Stages[1].Runs), 1, "stage1.runs")
	assert.Equal(t, len(plan.Stages[2].Runs), 1, "stage2.runs")
	assert.Equal(t, plan.Stages[0].Runs[0].JobID, "check", "jobid")
	assert.Equal(t, plan.Stages[1].Runs[0].JobID, "build", "jobid")
	assert.Equal(t, plan.Stages[2].Runs[0].JobID, "test", "jobid")

	plan, err = planner.PlanEvent("release")
	assert.NoError(t, err)
	assert.NotNil(t, plan)
	assert.Equal(t, 0, len(plan.Stages))
}

type TestJobFileInfo struct {
	workdir      string
	workflowPath string
	eventName    string
	errorMessage string
	platforms    map[string]string
	secrets      map[string]string
}

func (j *TestJobFileInfo) runTest(ctx context.Context, t *testing.T, cfg *Config) {
	fmt.Printf("::group::%s\n", j.workflowPath)

	log.SetLevel(logLevel)

	workdir, err := filepath.Abs(j.workdir)
	assert.Nil(t, err, workdir)

	fullWorkflowPath := filepath.Join(workdir, j.workflowPath)
	runnerConfig := &Config{
		Workdir:               workdir,
		BindWorkdir:           false,
		EventName:             j.eventName,
		EventPath:             cfg.EventPath,
		Platforms:             j.platforms,
		ReuseContainers:       false,
		Env:                   cfg.Env,
		Secrets:               cfg.Secrets,
		Inputs:                cfg.Inputs,
		GitHubInstance:        "github.com",
		ContainerArchitecture: cfg.ContainerArchitecture,
		Matrix:                cfg.Matrix,
		JobLoggerLevel:        cfg.JobLoggerLevel,
		ActionCache:           cfg.ActionCache,
	}

	runner, err := New(runnerConfig)
	assert.Nil(t, err, j.workflowPath)

	planner, err := model.NewWorkflowPlanner(fullWorkflowPath, true, false)
	if err != nil {
		assert.Error(t, err, j.errorMessage)
	} else {
		assert.Nil(t, err, fullWorkflowPath)

		plan, err := planner.PlanEvent(j.eventName)
		assert.True(t, (err == nil) != (plan == nil), "PlanEvent should return either a plan or an error")
		if err == nil && plan != nil {
			err = runner.NewPlanExecutor(plan)(ctx)
			if j.errorMessage == "" {
				assert.NoError(t, err, fullWorkflowPath)
			} else {
				require.Error(t, err, j.errorMessage)
				assert.ErrorContains(t, err, j.errorMessage)
			}
		}
	}

	fmt.Println("::endgroup::")
}

type TestConfig struct {
	LocalRepositories map[string]string `yaml:"local-repositories,omitempty"`
	Env               map[string]string `yaml:"env,omitempty"`
}

func TestRunner_RunEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	tables := []TestJobFileInfo{
		// Shells
		{workdir, "shells/defaults", "push", "", platforms, secrets},
		{workdir, "shells/custom", "push", "", platforms, secrets},
		{workdir, "shells/bash", "push", "", platforms, secrets},
		{workdir, "shells/node", "push", "", platforms, secrets},
		{workdir, "shells/python", "push", "", platforms, secrets},
		{workdir, "shells/sh", "push", "", platforms, secrets},
		{workdir, "shells/pwsh", "push", "", platforms, secrets},

		// Local action
		{workdir, "local-action-fails-schema-validation", "push", "Job 'test' failed", platforms, secrets},
		{workdir, "local-action-docker-url", "push", "", platforms, secrets},
		{workdir, "local-action-dockerfile", "push", "", platforms, secrets},
		{workdir + "/local-action-dockerfile-tag/example1", "local-action-dockerfile-example1", "push", "", platforms, secrets},
		{workdir + "/local-action-dockerfile-tag/example2", "local-action-dockerfile-example2", "push", "", platforms, secrets},
		{workdir, "local-action-via-composite-dockerfile", "push", "", platforms, secrets},
		{workdir, "local-action-js", "push", "", platforms, secrets},

		// Uses
		{workdir, "uses-composite", "push", "", platforms, secrets},
		{workdir, "uses-composite-with-error", "push", "Job 'failing-composite-action' failed", platforms, secrets},
		{workdir, "uses-composite-check-for-input-collision", "push", "", platforms, secrets},
		{workdir, "uses-composite-check-for-input-shadowing", "push", "", platforms, secrets},
		{workdir, "uses-nested-composite", "push", "", platforms, secrets},
		{workdir, "uses-composite-check-for-input-in-if-uses", "push", "", platforms, secrets},
		//		{workdir, "remote-action-composite-js-pre-with-defaults", "push", "", platforms, secrets},
		{workdir, "remote-action-composite-action-ref", "push", "", platforms, secrets},
		{workdir, "uses-workflow", "push", "", platforms, map[string]string{"secret": "keep_it_private"}},
		{workdir, "uses-workflow", "pull_request", "", platforms, map[string]string{"secret": "keep_it_private"}},
		{workdir, "uses-docker-url", "push", "", platforms, secrets},
		{workdir, "act-composite-env-test", "push", "", platforms, secrets},

		// Eval
		{workdir, "evalmatrix", "push", "", platforms, secrets},
		{workdir, "evalmatrixneeds", "push", "", platforms, secrets},
		{workdir, "evalmatrixneeds2", "push", "", platforms, secrets},
		{workdir, "evalmatrix-merge-map", "push", "", platforms, secrets},
		{workdir, "evalmatrix-merge-array", "push", "", platforms, secrets},

		{workdir, "basic", "push", "", platforms, secrets},
		{workdir, "timeout-minutes-stop", "push", "Job 'check' failed", platforms, secrets},
		{workdir, "timeout-minutes-job", "push", "context deadline exceeded", platforms, secrets},
		{workdir, "fail", "push", "Job 'build' failed", platforms, secrets},
		{workdir, "runs-on", "push", "", platforms, secrets},
		{workdir, "checkout", "push", "", platforms, secrets},
		{workdir, "job-container", "push", "", platforms, secrets},
		{workdir, "job-container-non-root", "push", "", platforms, secrets},
		{workdir, "job-container-invalid-credentials", "push", "failed to handle credentials: failed to interpolate container.credentials.password", platforms, secrets},
		{workdir, "container-hostname", "push", "", platforms, secrets},
		{workdir, "remote-action-docker", "push", "", platforms, secrets},
		{workdir, "remote-action-js", "push", "", platforms, secrets},
		// {workdir, "remote-action-js-node-user", "push", "", platforms, secrets}, // Test if this works with non root container
		{workdir, "matrix", "push", "", platforms, secrets},
		{workdir, "matrix-include-exclude", "push", "", platforms, secrets},
		{workdir, "matrix-exitcode", "push", "Job 'test' failed", platforms, secrets},
		{workdir, "matrix-shell", "push", "", platforms, secrets},
		{workdir, "commands", "push", "", platforms, secrets},
		{workdir, "workdir", "push", "", platforms, secrets},
		{workdir, "defaults-run", "push", "", platforms, secrets},
		{workdir, "composite-fail-with-output", "push", "", platforms, secrets},
		{workdir, "issue-597", "push", "", platforms, secrets},
		{workdir, "issue-598", "push", "", platforms, secrets},
		{workdir, "if-env-act", "push", "", platforms, secrets},
		{workdir, "env-and-path", "push", "", platforms, secrets},
		{workdir, "environment-files", "push", "", platforms, secrets},
		{workdir, "GITHUB_STATE", "push", "", platforms, secrets},
		{workdir, "environment-files-parser-bug", "push", "", platforms, secrets},
		{workdir, "non-existent-action", "push", "Job 'nopanic' failed", platforms, secrets},
		{workdir, "outputs", "push", "", platforms, secrets},
		{workdir, "networking", "push", "", platforms, secrets},
		{workdir, "steps-context/conclusion", "push", "", platforms, secrets},
		{workdir, "steps-context/outcome", "push", "", platforms, secrets},
		{workdir, "job-status-check", "push", "Job 'fail' failed", platforms, secrets},
		{workdir, "if-expressions", "push", "Job 'mytest' failed", platforms, secrets},
		{workdir, "actions-environment-and-context-tests", "push", "", platforms, secrets},
		{workdir, "uses-action-with-pre-and-post-step", "push", "", platforms, secrets},
		{workdir, "evalenv", "push", "", platforms, secrets},
		{workdir, "docker-action-custom-path", "push", "", platforms, secrets},
		{workdir, "GITHUB_ENV-use-in-env-ctx", "push", "", platforms, secrets},
		{workdir, "ensure-post-steps", "push", "Job 'second-post-step-should-fail' failed", platforms, secrets},
		{workdir, "workflow_call_inputs", "workflow_call", "", platforms, secrets},
		{workdir, "workflow_dispatch", "workflow_dispatch", "", platforms, secrets},
		{workdir, "workflow_dispatch_no_inputs_mapping", "workflow_dispatch", "", platforms, secrets},
		{workdir, "workflow_dispatch-scalar", "workflow_dispatch", "", platforms, secrets},
		{workdir, "workflow_dispatch-scalar-composite-action", "workflow_dispatch", "", platforms, secrets},
		{workdir, "uses-workflow-defaults", "workflow_dispatch", "", platforms, secrets},
		{workdir, "job-needs-context-contains-result", "push", "", platforms, secrets},
		{"../model/testdata", "strategy", "push", "", platforms, secrets}, // TODO: move all testdata into pkg so we can validate it with planner and runner
		{workdir, "path-handling", "push", "", platforms, secrets},
		{workdir, "do-not-leak-step-env-in-composite", "push", "", platforms, secrets},
		{workdir, "set-env-step-env-override", "push", "", platforms, secrets},
		{workdir, "set-env-new-env-file-per-step", "push", "", platforms, secrets},
		{workdir, "no-panic-on-invalid-composite-action", "push", "missing steps in composite action", platforms, secrets},
		{workdir, "stepsummary", "push", "", platforms, secrets},
		{workdir, "tool-cache", "push", "", platforms, secrets},

		// services
		{workdir, "services", "push", "", platforms, secrets},
		{workdir, "services-with-container", "push", "", platforms, secrets},
		{workdir, "mysql-service-container-with-health-check", "push", "", platforms, secrets},
		{workdir, "mysql-service-container-premature-terminate", "push", "service [maindb]", platforms, secrets},
	}

	for _, table := range tables {
		t.Run(table.workflowPath, func(t *testing.T) {
			config := &Config{
				Secrets: table.secrets,
			}

			eventFile := filepath.Join(workdir, table.workflowPath, "event.json")
			if _, err := os.Stat(eventFile); err == nil {
				config.EventPath = eventFile
			}

			testConfigFile := filepath.Join(workdir, table.workflowPath, "config/config.yml")
			if file, err := os.ReadFile(testConfigFile); err == nil {
				testConfig := &TestConfig{}
				if yaml.Unmarshal(file, testConfig) == nil {
					if testConfig.LocalRepositories != nil {
						config.ActionCache = &LocalRepositoryCache{
							Parent: GoGitActionCache{
								path.Clean(path.Join(workdir, "cache")),
							},
							LocalRepositories: testConfig.LocalRepositories,
							CacheDirCache:     map[string]string{},
						}
					}
				}
				config.Env = testConfig.Env
			}

			table.runTest(ctx, t, config)
		})
	}
}

func TestRunner_DryrunEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.WithDryrun(t.Context(), true)

	tables := []TestJobFileInfo{
		// Shells
		{workdir, "shells/defaults", "push", "", platforms, secrets},
		{workdir, "shells/pwsh", "push", "", map[string]string{"ubuntu-latest": "catthehacker/ubuntu:pwsh-latest"}, secrets}, // custom image with pwsh
		{workdir, "shells/bash", "push", "", platforms, secrets},
		{workdir, "shells/python", "push", "", map[string]string{"ubuntu-latest": "node:16-buster"}, secrets}, // slim doesn't have python
		{workdir, "shells/sh", "push", "", platforms, secrets},

		// Local action
		{workdir, "local-action-docker-url", "push", "", platforms, secrets},
		{workdir, "local-action-dockerfile", "push", "", platforms, secrets},
		{workdir, "local-action-via-composite-dockerfile", "push", "", platforms, secrets},
		{workdir, "local-action-js", "push", "", platforms, secrets},
	}

	for _, table := range tables {
		t.Run(table.workflowPath, func(t *testing.T) {
			table.runTest(ctx, t, &Config{})
		})
	}
}

func TestRunner_DockerActionForcePullForceRebuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	config := &Config{
		ForcePull:    true,
		ForceRebuild: true,
	}

	tables := []TestJobFileInfo{
		{workdir, "local-action-dockerfile", "push", "", platforms, secrets},
		{workdir, "local-action-via-composite-dockerfile", "push", "", platforms, secrets},
	}

	for _, table := range tables {
		t.Run(table.workflowPath, func(t *testing.T) {
			table.runTest(ctx, t, config)
		})
	}
}

func TestRunner_RunDifferentArchitecture(t *testing.T) {
	t.Skip("Flaky see https://code.forgejo.org/forgejo/runner/issues/763")
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tjfi := TestJobFileInfo{
		workdir:      workdir,
		workflowPath: "basic",
		eventName:    "push",
		errorMessage: "",
		platforms:    platforms,
	}

	tjfi.runTest(t.Context(), t, &Config{ContainerArchitecture: "linux/arm64"})
}

type runSkippedHook struct {
	resultKey string
	found     bool
}

func (h *runSkippedHook) Levels() []log.Level {
	return []log.Level{log.InfoLevel}
}

func (h *runSkippedHook) Fire(entry *log.Entry) error {
	if result, ok := entry.Data[h.resultKey]; ok {
		h.found = (fmt.Sprintf("%s", result) == "skipped")
	}
	return nil
}

func TestRunner_RunSkipped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	for _, what := range []string{"step", "job"} {
		t.Run(what, func(t *testing.T) {
			tjfi := TestJobFileInfo{
				workdir:      workdir,
				workflowPath: "skip" + what,
				eventName:    "push",
				errorMessage: "",
				platforms:    platforms,
			}

			h := &runSkippedHook{resultKey: what + "Result"}
			ctx := common.WithLoggerHook(t.Context(), h)

			jobLoggerLevel := log.InfoLevel
			tjfi.runTest(ctx, t, &Config{ContainerArchitecture: "linux/arm64", JobLoggerLevel: &jobLoggerLevel})

			assert.True(t, h.found)
		})
	}
}

type maskJobLoggerFactory struct {
	Output syncBuffer
}

type syncBuffer struct {
	buffer bytes.Buffer
	mutex  sync.Mutex
}

func (sb *syncBuffer) Write(p []byte) (n int, err error) {
	sb.mutex.Lock()
	defer sb.mutex.Unlock()
	return sb.buffer.Write(p)
}

func (sb *syncBuffer) String() string {
	sb.mutex.Lock()
	defer sb.mutex.Unlock()
	return sb.buffer.String()
}

func (f *maskJobLoggerFactory) WithJobLogger() *log.Logger {
	logger := log.New()
	logger.SetOutput(io.MultiWriter(&f.Output, os.Stdout))
	logger.SetLevel(log.DebugLevel)
	return logger
}

func TestRunner_MaskValues(t *testing.T) {
	assertNoSecret := func(text, secret string) {
		index := strings.Index(text, "composite secret")
		if index > -1 {
			fmt.Printf("\nFound Secret in the given text:\n%s\n", text)
		}
		assert.False(t, strings.Contains(text, "composite secret"))
	}

	if testing.Short() {
		t.Skip("skipping integration test")
	}

	log.SetLevel(log.DebugLevel)

	tjfi := TestJobFileInfo{
		workdir:      workdir,
		workflowPath: "mask-values",
		eventName:    "push",
		errorMessage: "",
		platforms:    platforms,
	}

	logger := &maskJobLoggerFactory{}
	tjfi.runTest(WithJobLoggerFactory(common.WithLogger(t.Context(), logger.WithJobLogger()), logger), t, &Config{})
	output := logger.Output.String()

	assertNoSecret(output, "secret value")
	assertNoSecret(output, "YWJjCg==")
}

func TestRunner_RunEventSecrets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	workflowPath := "secrets"

	tjfi := TestJobFileInfo{
		workdir:      workdir,
		workflowPath: workflowPath,
		eventName:    "push",
		errorMessage: "",
		platforms:    platforms,
	}

	env, err := godotenv.Read(filepath.Join(workdir, workflowPath, ".env"))
	assert.NoError(t, err, "Failed to read .env")
	secrets, _ := godotenv.Read(filepath.Join(workdir, workflowPath, ".secrets"))
	assert.NoError(t, err, "Failed to read .secrets")

	tjfi.runTest(t.Context(), t, &Config{Secrets: secrets, Env: env})
}

func TestRunner_RunWithService(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	log.SetLevel(log.DebugLevel)
	ctx := t.Context()

	platforms := map[string]string{
		"ubuntu-latest": "code.forgejo.org/oci/node:22",
	}

	workflowPath := "services"
	eventName := "push"

	workdir, err := filepath.Abs("testdata")
	assert.NoError(t, err, workflowPath)

	runnerConfig := &Config{
		Workdir:         workdir,
		EventName:       eventName,
		Platforms:       platforms,
		ReuseContainers: false,
	}
	runner, err := New(runnerConfig)
	assert.NoError(t, err, workflowPath)

	planner, err := model.NewWorkflowPlanner(fmt.Sprintf("testdata/%s", workflowPath), true, false)
	assert.NoError(t, err, workflowPath)

	plan, err := planner.PlanEvent(eventName)
	assert.NoError(t, err, workflowPath)

	err = runner.NewPlanExecutor(plan)(ctx)
	assert.NoError(t, err, workflowPath)
}

func TestRunner_RunActionInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	workflowPath := "input-from-cli"

	tjfi := TestJobFileInfo{
		workdir:      workdir,
		workflowPath: workflowPath,
		eventName:    "workflow_dispatch",
		errorMessage: "",
		platforms:    platforms,
	}

	inputs := map[string]string{
		"SOME_INPUT": "input",
	}

	tjfi.runTest(t.Context(), t, &Config{Inputs: inputs})
}

func TestRunner_RunEventPullRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	workflowPath := "pull-request"

	tjfi := TestJobFileInfo{
		workdir:      workdir,
		workflowPath: workflowPath,
		eventName:    "pull_request",
		errorMessage: "",
		platforms:    platforms,
	}

	tjfi.runTest(t.Context(), t, &Config{EventPath: filepath.Join(workdir, workflowPath, "event.json")})
}

func TestRunner_RunMatrixWithUserDefinedInclusions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	workflowPath := "matrix-with-user-inclusions"

	tjfi := TestJobFileInfo{
		workdir:      workdir,
		workflowPath: workflowPath,
		eventName:    "push",
		errorMessage: "",
		platforms:    platforms,
	}

	matrix := map[string]map[string]bool{
		"node": {
			"8":   true,
			"8.x": true,
		},
		"os": {
			"ubuntu-18.04": true,
		},
	}

	tjfi.runTest(t.Context(), t, &Config{Matrix: matrix})
}

// Regression test against `runs-on` in a matrix run, which references the matrix values, being corrupted by multiple
// concurrent goroutines.
func TestRunner_RunsOnMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	log.SetLevel(log.DebugLevel)

	tjfi := TestJobFileInfo{
		workdir:      workdir,
		workflowPath: "matrix-runs-on",
		eventName:    "push",
		errorMessage: "",
		platforms:    platforms,
	}

	logger := &maskJobLoggerFactory{}
	tjfi.runTest(WithJobLoggerFactory(common.WithLogger(t.Context(), logger.WithJobLogger()), logger), t, &Config{})
	output := logger.Output.String()

	// job 1 should succeed because `ubuntu-latest` is a valid runs-on target...
	assert.Contains(t, output, "msg=\"🏁  Job succeeded\" dryrun=false job=test/matrix-runs-on-1", "expected job 1 to succeed, but did not find success message")

	// job 2 should be skipped because `ubuntu-20.04` is not a valid runs-on target in `platforms`.
	assert.Contains(t, output, "msg=\"🚧  Skipping unsupported platform -- Try running with `-P ubuntu-20.04=...`\" dryrun=false job=test/matrix-runs-on-2", "expected job 2 to be skipped, but it was not")
}

package runner

import (
	"context"
	"fmt"
	"time"

	"code.forgejo.org/forgejo/runner/v9/act/common"
	"code.forgejo.org/forgejo/runner/v9/act/container"
	"code.forgejo.org/forgejo/runner/v9/act/model"
)

type jobInfo interface {
	matrix() map[string]any
	steps() []*model.Step
	startContainer() common.Executor
	stopContainer() common.Executor
	closeContainer() common.Executor
	interpolateOutputs() common.Executor
	result(result string)
}

const cleanupTimeout = 30 * time.Minute

func newJobExecutor(info jobInfo, sf stepFactory, rc *RunContext) common.Executor {
	steps := make([]common.Executor, 0)
	preSteps := make([]common.Executor, 0)
	var postExecutor common.Executor

	steps = append(steps, func(ctx context.Context) error {
		logger := common.Logger(ctx)
		if len(info.matrix()) > 0 {
			logger.Infof("\U0001F9EA  Matrix: %v", info.matrix())
		}
		return nil
	})

	infoSteps := info.steps()

	if len(infoSteps) == 0 {
		return common.NewDebugExecutor("No steps found")
	}

	preSteps = append(preSteps, func(ctx context.Context) error {
		// Have to be skipped for some Tests
		if rc.Run == nil {
			return nil
		}
		rc.ExprEval = rc.NewExpressionEvaluator(ctx)
		// evaluate environment variables since they can contain
		// GitHub's special environment variables.
		for k, v := range rc.GetEnv() {
			rc.Env[k] = rc.ExprEval.Interpolate(ctx, v)
		}
		return nil
	})

	for i, stepModel := range infoSteps {
		if stepModel == nil {
			return func(ctx context.Context) error {
				return fmt.Errorf("invalid Step %v: missing run or uses key", i)
			}
		}
		if stepModel.ID == "" {
			stepModel.ID = fmt.Sprintf("%d", i)
		}
		stepModel.Number = i

		step, err := sf.newStep(stepModel, rc)
		if err != nil {
			return common.NewErrorExecutor(err)
		}

		preExec := step.pre()
		preSteps = append(preSteps, useStepLogger(rc, stepModel, stepStagePre, func(ctx context.Context) error {
			logger := common.Logger(ctx)
			preErr := preExec(ctx)
			if preErr != nil {
				logger.Errorf("%v", preErr)
				common.SetJobError(ctx, preErr)
			} else if ctx.Err() != nil {
				logger.Errorf("%v", ctx.Err())
				common.SetJobError(ctx, ctx.Err())
			}
			return preErr
		}))

		stepExec := step.main()
		steps = append(steps, useStepLogger(rc, stepModel, stepStageMain, func(ctx context.Context) error {
			logger := common.Logger(ctx)
			err := stepExec(ctx)
			if err != nil {
				logger.Errorf("%v", err)
				common.SetJobError(ctx, err)
			} else if ctx.Err() != nil {
				logger.Errorf("%v", ctx.Err())
				common.SetJobError(ctx, ctx.Err())
			}
			return nil
		}))

		postExec := useStepLogger(rc, stepModel, stepStagePost, step.post())
		if postExecutor != nil {
			// run the post executor in reverse order
			postExecutor = postExec.Finally(postExecutor)
		} else {
			postExecutor = postExec
		}
	}

	postExecutor = postExecutor.Finally(func(ctx context.Context) error {
		jobError := common.JobError(ctx)

		// Fresh context to ensure job result output works even if prev. context was a cancelled job
		ctx, cancel := context.WithTimeout(common.WithLogger(context.Background(), common.Logger(ctx)), time.Minute)
		defer cancel()
		setJobResult(ctx, info, rc, jobError == nil)
		setJobOutputs(ctx, rc)

		var err error
		{
			// Separate timeout for cleanup tasks; logger is cleared so that cleanup logs go to runner, not job
			ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
			defer cancel()

			logger := common.Logger(ctx)
			logger.Debugf("Cleaning up container for job %s", rc.jobContainerName())
			if err = info.stopContainer()(ctx); err != nil {
				logger.Errorf("Error while stop job container %s: %v", rc.jobContainerName(), err)
			}

			if !rc.IsHostEnv(ctx) && rc.Config.ContainerNetworkMode == "" {
				// clean network in docker mode only
				// if the value of `ContainerNetworkMode` is empty string,
				// it means that the network to which containers are connecting is created by `act_runner`,
				// so, we should remove the network at last.
				networkName, _ := rc.networkName()
				logger.Debugf("Cleaning up network %s for job %s", networkName, rc.jobContainerName())
				if err := container.NewDockerNetworkRemoveExecutor(networkName)(ctx); err != nil {
					logger.Errorf("Error while cleaning network %s: %v", networkName, err)
				}
			}
		}
		return err
	})

	pipeline := make([]common.Executor, 0)
	pipeline = append(pipeline, preSteps...)
	pipeline = append(pipeline, steps...)

	return common.NewPipelineExecutor(info.startContainer(), common.NewPipelineExecutor(pipeline...).
		Finally(func(ctx context.Context) error { //nolint:contextcheck
			var cancel context.CancelFunc
			if ctx.Err() == context.Canceled {
				// in case of an aborted run, we still should execute the
				// post steps to allow cleanup.
				ctx, cancel = context.WithTimeout(common.WithLogger(context.Background(), common.Logger(ctx)), cleanupTimeout)
				defer cancel()
			}
			return postExecutor(ctx)
		}).
		Finally(info.interpolateOutputs()).
		Finally(info.closeContainer()))
}

func setJobResult(ctx context.Context, info jobInfo, rc *RunContext, success bool) {
	logger := common.Logger(ctx)

	jobResult := "success"
	// we have only one result for a whole matrix build, so we need
	// to keep an existing result state if we run a matrix
	if len(info.matrix()) > 0 && rc.Run.Job().Result != "" {
		jobResult = rc.Run.Job().Result
	}

	if !success {
		jobResult = "failure"
	}

	info.result(jobResult)
	if rc.caller != nil {
		// set reusable workflow job result
		rc.caller.runContext.result(jobResult)
	}

	jobResultMessage := "succeeded"
	if jobResult != "success" {
		jobResultMessage = "failed"
	}

	logger.WithField("jobResult", jobResult).Infof("\U0001F3C1  Job %s", jobResultMessage)
}

func setJobOutputs(ctx context.Context, rc *RunContext) {
	if rc.caller != nil {
		// map outputs for reusable workflows
		callerOutputs := make(map[string]string)

		ee := rc.NewExpressionEvaluator(ctx)

		for k, v := range rc.Run.Workflow.WorkflowCallConfig().Outputs {
			callerOutputs[k] = ee.Interpolate(ctx, ee.Interpolate(ctx, v.Value))
		}

		rc.caller.runContext.Run.Job().Outputs = callerOutputs
	}
}

func useStepLogger(rc *RunContext, stepModel *model.Step, stage stepStage, executor common.Executor) common.Executor {
	return func(ctx context.Context) error {
		ctx = withStepLogger(ctx, stepModel.Number, stepModel.ID, rc.ExprEval.Interpolate(ctx, stepModel.String()), stage.String())

		rawLogger := common.Logger(ctx).WithField("raw_output", true)
		logWriter := common.NewLineWriter(rc.commandHandler(ctx), func(s string) bool {
			if rc.Config.LogOutput {
				rawLogger.Infof("%s", s)
			} else {
				rawLogger.Debugf("%s", s)
			}
			return true
		})

		oldout, olderr := rc.JobContainer.ReplaceLogWriter(logWriter, logWriter)
		defer rc.JobContainer.ReplaceLogWriter(oldout, olderr)

		return executor(ctx)
	}
}

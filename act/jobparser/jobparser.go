package jobparser

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"code.forgejo.org/forgejo/runner/v12/act/exprparser"
	"code.forgejo.org/forgejo/runner/v12/act/model"
)

func Parse(content []byte, validate bool, options ...ParseOption) ([]*SingleWorkflow, error) {
	workflow := &SingleWorkflow{}
	if err := yaml.Unmarshal(content, workflow); err != nil {
		return nil, fmt.Errorf("yaml.Unmarshal: %w", err)
	}
	if workflow.IncompleteMatrix {
		// The IncompleteMatrix tag can't be unmarshaled into `model.Workflow`; it's only applicable for
		// `SingleWorkflow`.  If it's being passed back to `Parse` in the `content` field, then we're currently trying
		// to expand the incomplete matrix from a `SingleWorkflow` that we previously generated.
		//
		// To allow the creation of `origin`, remove this field...
		workflow.IncompleteMatrix = false
		workflow.IncompleteMatrixNeeds = nil
		rewrittenContent, err := workflow.Marshal()
		if err != nil {
			return nil, err
		}
		content = rewrittenContent
	}

	origin, err := model.ReadWorkflow(bytes.NewReader(content), validate)
	if err != nil {
		return nil, fmt.Errorf("model.ReadWorkflow: %w", err)
	}

	pc := &parseContext{}
	for _, o := range options {
		o(pc)
	}
	results := map[string]*JobResult{}
	for id, job := range origin.Jobs {
		results[id] = &JobResult{
			Needs:   job.Needs(),
			Result:  pc.jobResults[id],
			Outputs: pc.jobOutputs[id],
		}
	}
	// See documentation on `WithWorkflowNeeds` for why we do this:
	for _, id := range pc.workflowNeeds {
		results[id] = &JobResult{
			Result:  pc.jobResults[id],
			Outputs: pc.jobOutputs[id],
		}
	}
	incompleteMatrix := make(map[string]*exprparser.InvalidJobOutputReferencedError) // map job id -> incomplete matrix reason
	for id, job := range origin.Jobs {
		if job.Strategy != nil {
			jobNeeds := pc.workflowNeeds
			if jobNeeds == nil {
				jobNeeds = job.Needs()
			}
			matrixEvaluator := NewExpressionEvaluator(NewInterpreter(id, job, nil, pc.gitContext, results, pc.vars, pc.inputs, exprparser.InvalidJobOutput, jobNeeds))
			if err := matrixEvaluator.EvaluateYamlNode(&job.Strategy.RawMatrix); err != nil {
				// IncompleteMatrix tagging is only supported when `WithJobOutputs()` is used as an option, in order to
				// maintain jobparser's backwards compatibility.
				var perr *exprparser.InvalidJobOutputReferencedError
				if pc.jobOutputs != nil && errors.As(err, &perr) {
					incompleteMatrix[id] = perr
				} else {
					return nil, fmt.Errorf("failure to evaluate strategy.matrix on job %s: %w", job.Name, err)
				}
			}
		}
	}

	var ret []*SingleWorkflow
	ids, jobs, err := workflow.jobs()
	if err != nil {
		return nil, fmt.Errorf("invalid jobs: %w", err)
	}
	for i, id := range ids {
		job := jobs[i]
		jobNeeds := pc.workflowNeeds
		if jobNeeds == nil {
			jobNeeds = job.Needs()
		}
		matricxes, err := getMatrixes(origin.GetJob(id))
		if err != nil {
			return nil, fmt.Errorf("getMatrixes: %w", err)
		}
		if incompleteMatrix[id] != nil {
			// If this job is IncompleteMatrix, then ensure that the matrices for the job are undefined.  Otherwise if
			// there's an array like `[value1, ${{ needs... }}]` then multiple IncompleteMatrix jobs will be emitted.
			matricxes = []map[string]any{{}}
		}
		for _, matrix := range matricxes {
			job := job.Clone()
			evaluator := NewExpressionEvaluator(NewInterpreter(id, origin.GetJob(id), matrix, pc.gitContext, results, pc.vars, pc.inputs, 0, jobNeeds))

			if incompleteMatrix[id] != nil {
				// Preserve the original incomplete `matrix` value so that when the `IncompleteMatrix` state is
				// discovered later, it can be expanded.
				job.Strategy.RawMatrix = origin.GetJob(id).Strategy.RawMatrix
			} else {
				job.Strategy.RawMatrix = encodeMatrix(matrix)
			}

			// If we're IncompleteMatrix, don't compute the job name -- this will allow it to remain blank and be
			// computed when the matrix is expanded in a future reparse.
			if incompleteMatrix[id] == nil {
				if job.Name == "" {
					job.Name = nameWithMatrix(id, matrix)
				} else if strings.HasSuffix(job.Name, " (incomplete matrix)") {
					job.Name = nameWithMatrix(strings.TrimSuffix(job.Name, " (incomplete matrix)"), matrix)
				} else {
					job.Name = evaluator.Interpolate(job.Name)
				}
			} else {
				if job.Name == "" {
					job.Name = nameWithMatrix(id, matrix) + " (incomplete matrix)"
				} else {
					job.Name = evaluator.Interpolate(job.Name) + " (incomplete matrix)"
				}
			}

			var runsOnInvalidJobReference *exprparser.InvalidJobOutputReferencedError
			var runsOn []string
			if pc.supportIncompleteRunsOn {
				evaluatorOutputAware := NewExpressionEvaluator(NewInterpreter(id, origin.GetJob(id), matrix, pc.gitContext, results, pc.vars, pc.inputs, exprparser.InvalidJobOutput, jobNeeds))
				rawRunsOn := origin.GetJob(id).RawRunsOn
				// Evaluate the entire `runs-on` node at once, which permits behavior like `runs-on: ${{ fromJSON(...)
				// }}` where it can generate an array
				err = evaluatorOutputAware.EvaluateYamlNode(&rawRunsOn)
				if err != nil {
					// Store error and we'll use it to tag `IncompleteRunsOn`
					errors.As(err, &runsOnInvalidJobReference)
				}
				runsOn = model.FlattenRunsOnNode(rawRunsOn)
			} else {
				// Legacy behaviour; run interpolator on each individual entry in the `runsOn` array without support for
				// `IncompleteRunsOn` detection:
				runsOn = origin.GetJob(id).RunsOn()
				for i, v := range runsOn {
					runsOn[i] = evaluator.Interpolate(v)
				}
			}

			job.RawRunsOn = encodeRunsOn(runsOn)
			swf := &SingleWorkflow{
				Name:     workflow.Name,
				RawOn:    workflow.RawOn,
				Env:      workflow.Env,
				Defaults: workflow.Defaults,
			}
			if refErr := incompleteMatrix[id]; refErr != nil {
				swf.IncompleteMatrix = true
				swf.IncompleteMatrixNeeds = &IncompleteNeeds{
					Job:    refErr.JobID,
					Output: refErr.OutputName,
				}
			}
			if runsOnInvalidJobReference != nil {
				swf.IncompleteRunsOn = true
				swf.IncompleteRunsOnNeeds = &IncompleteNeeds{
					Job:    runsOnInvalidJobReference.JobID,
					Output: runsOnInvalidJobReference.OutputName,
				}
			}
			if err := swf.SetJob(id, job); err != nil {
				return nil, fmt.Errorf("SetJob: %w", err)
			}
			ret = append(ret, swf)
		}
	}
	return ret, nil
}

func WithJobResults(results map[string]string) ParseOption {
	return func(c *parseContext) {
		c.jobResults = results
	}
}

func WithJobOutputs(outputs map[string]map[string]string) ParseOption {
	return func(c *parseContext) {
		c.jobOutputs = outputs
	}
}

func WithGitContext(context *model.GithubContext) ParseOption {
	return func(c *parseContext) {
		c.gitContext = context
	}
}

func WithInputs(inputs map[string]any) ParseOption {
	return func(c *parseContext) {
		c.inputs = inputs
	}
}

func WithVars(vars map[string]string) ParseOption {
	return func(c *parseContext) {
		c.vars = vars
	}
}

func SupportIncompleteRunsOn() ParseOption {
	return func(c *parseContext) {
		c.supportIncompleteRunsOn = true
	}
}

// `WithWorkflowNeeds` allows overridding the `needs` field for a job being parsed.
//
// In the case that a `SingleWorkflow`, returned from `Parse`, is passed back into `Parse` later in order to expand its
// IncompleteMatrix, then the jobs that it needs will not be present in the workflow (because `SingleWorkflow` only has
// one job in it).  The `needs` field on the job itself may also be absent (Forgejo truncates the `needs` so that it can
// coordinate dispatching the jobs one-by-one without the runner panicing over missing jobs). However, the `needs` field
// is needed in order to populate the `needs` variable context. `WithWorkflowNeeds` can be used to indicate the needs
// exist and are fulfilled.
func WithWorkflowNeeds(needs []string) ParseOption {
	return func(c *parseContext) {
		c.workflowNeeds = needs
	}
}

type parseContext struct {
	jobResults              map[string]string
	jobOutputs              map[string]map[string]string // map job ID -> output key -> output value
	gitContext              *model.GithubContext
	inputs                  map[string]any
	vars                    map[string]string
	workflowNeeds           []string
	supportIncompleteRunsOn bool
}

type ParseOption func(c *parseContext)

func getMatrixes(job *model.Job) ([]map[string]any, error) {
	ret, err := job.GetMatrixes()
	if err != nil {
		return nil, fmt.Errorf("GetMatrixes: %w", err)
	}
	sort.Slice(ret, func(i, j int) bool {
		return matrixName(ret[i]) < matrixName(ret[j])
	})
	return ret, nil
}

func encodeMatrix(matrix map[string]any) yaml.Node {
	if len(matrix) == 0 {
		return yaml.Node{}
	}
	value := map[string][]any{}
	for k, v := range matrix {
		value[k] = []any{v}
	}
	node := yaml.Node{}
	_ = node.Encode(value)
	return node
}

func encodeRunsOn(runsOn []string) yaml.Node {
	node := yaml.Node{}
	if len(runsOn) == 1 {
		_ = node.Encode(runsOn[0])
	} else {
		_ = node.Encode(runsOn)
	}
	return node
}

func nameWithMatrix(name string, m map[string]any) string {
	if len(m) == 0 {
		return name
	}

	return name + " " + matrixName(m)
}

func matrixName(m map[string]any) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	vs := make([]string, 0, len(m))
	for _, v := range ks {
		vs = append(vs, fmt.Sprint(m[v]))
	}

	return fmt.Sprintf("(%s)", strings.Join(vs, ", "))
}

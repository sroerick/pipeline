package graph

import (
	"fmt"
	"slices"

	"pakkun/internal/pipeline"
)

func TopoSort(p pipeline.Pipeline) ([]pipeline.Step, error) {
	steps := map[string]pipeline.Step{}
	inDegree := map[string]int{}
	outgoing := map[string][]string{}
	for _, step := range p.Steps {
		steps[step.Name] = step
		inDegree[step.Name] = 0
	}
	for _, step := range p.Steps {
		for _, input := range step.Inputs {
			if input.From == "" {
				continue
			}
			fromStep, _, err := pipeline.ParseStepOutputRef(input.From)
			if err != nil {
				return nil, err
			}
			outgoing[fromStep] = append(outgoing[fromStep], step.Name)
			inDegree[step.Name]++
		}
	}
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	slices.Sort(queue)
	var result []pipeline.Step
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, steps[current])
		nextSteps := outgoing[current]
		slices.Sort(nextSteps)
		for _, next := range nextSteps {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
				slices.Sort(queue)
			}
		}
	}
	if len(result) != len(p.Steps) {
		return nil, fmt.Errorf("cycle detected in pipeline %q", p.Name)
	}
	return result, nil
}

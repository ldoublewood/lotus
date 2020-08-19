package sectorstorage

import (
	"context"

	"github.com/filecoin-project/specs-actors/actors/abi"

	"github.com/filecoin-project/lotus/extern/sector-storage/sealtasks"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
)

type taskSelector struct {
	best []stores.StorageInfo //nolint: unused, structcheck
}

func newTaskSelector() *taskSelector {
	return &taskSelector{}
}

func (s *taskSelector) Ok(ctx context.Context, task sealtasks.TaskType, spt abi.RegisteredSealProof, whnd *workerHandle) (bool, error) {
	tasks := whnd.acceptTasks
	_, supported := tasks[task]

	return supported, nil
}

func (s *taskSelector) Cmp(ctx context.Context, _ sealtasks.TaskType, a, b *workerHandle) (bool, error) {
	atasks := a.acceptTasks
	btasks := b.acceptTasks

	if len(atasks) != len(btasks) {
		return len(atasks) < len(btasks), nil // prefer workers which can do less
	}

	return a.active.utilization(a.info.Resources) < b.active.utilization(b.info.Resources), nil
}

var _ WorkerSelector = &allocSelector{}

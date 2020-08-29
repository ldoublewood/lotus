package sectorstorage

import (
	"sync"

	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
)

func (a *activeResources) withResources(id WorkerID, wr storiface.WorkerResources, r Resources, locker sync.Locker, cb func() error) error {
	for !a.canHandleRequest(r, id, "withResources", wr) {
		if a.cond == nil {
			a.cond = sync.NewCond(locker)
		}
		a.cond.Wait()
	}

	used := a.add(wr, r)

	err := cb()

	a.free(used)
	if a.cond != nil {
		a.cond.Broadcast()
	}

	return err
}

func (a *activeResources) add(wr storiface.WorkerResources, r Resources) *usedResources {
	cpu, gpu := getNeedGpuCpu(r, wr, a)
	a.cpuUse += cpu
	a.gpuUse += gpu
	a.memUsedMin += r.MinMemory
	a.memUsedMax += r.MaxMemory
	return &usedResources{
		memUsedMin: r.MinMemory,
		memUsedMax: r.MaxMemory,
		cpuUse:     cpu,
		gpuUse:     gpu,
	}
}

func (a *activeResources) free(used *usedResources) {
	a.memUsedMin -= used.memUsedMin
	a.memUsedMax -= used.memUsedMax
	a.cpuUse -= used.cpuUse
	a.gpuUse -= used.gpuUse
}

func (a *activeResources) canHandleRequest(needRes Resources, wid WorkerID, caller string, res storiface.WorkerResources) bool {

	// TODO: dedupe needRes.BaseMinMemory per task type (don't add if that task is already running)
	minNeedMem := res.MemReserved + a.memUsedMin + needRes.MinMemory + needRes.BaseMinMemory
	if minNeedMem > res.MemPhysical {
		log.Debugf("sched: not scheduling on worker %d for %s; not enough physical memory - need: %dM, have %dM", wid, caller, minNeedMem/mib, res.MemPhysical/mib)
		return false
	}

	maxNeedMem := res.MemReserved + a.memUsedMax + needRes.MaxMemory + needRes.BaseMinMemory

	if maxNeedMem > res.MemSwap+res.MemPhysical {
		log.Debugf("sched: not scheduling on worker %d for %s; not enough virtual memory - need: %dM, have %dM", wid, caller, maxNeedMem/mib, (res.MemSwap+res.MemPhysical)/mib)
		return false
	}
	needCpu, needGpu := getNeedGpuCpu(needRes, res, a)
	gpus := getVirtualGpu(res)

	if a.cpuUse+needCpu > res.CPUs {
		log.Debugf("sched: not scheduling on worker %d; not enough threads, need %d, %d in use, target %d", wid, needRes.Threads, a.cpuUse, res.CPUs)
		return false
	}
	if a.gpuUse+needGpu > gpus {
		log.Debugf("sched: not scheduling on worker %d; not enough threads, need %d, %d in use, target %d", wid, needRes.Threads, a.gpuUse, len(res.GPUs))
		return false
	}

	return true
}

func (a *activeResources) utilization(wr storiface.WorkerResources) float64 {
	var max float64

	cpu := float64(a.cpuUse) / float64(wr.CPUs)
	max = cpu

	memMin := float64(a.memUsedMin+wr.MemReserved) / float64(wr.MemPhysical)
	if memMin > max {
		max = memMin
	}

	memMax := float64(a.memUsedMax+wr.MemReserved) / float64(wr.MemPhysical+wr.MemSwap)
	if memMax > max {
		max = memMax
	}

	return max
}

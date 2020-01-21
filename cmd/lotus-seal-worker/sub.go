package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	paramfetch "github.com/filecoin-project/go-paramfetch"
	"github.com/filecoin-project/go-sectorbuilder"
	"golang.org/x/xerrors"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
)

type worker struct {
	api           lapi.StorageMiner
	minerEndpoint string
	repo          string
	auth          http.Header

	sb *sectorbuilder.SectorBuilder
}

func acceptJobs(ctx context.Context, api lapi.StorageMiner, endpoint string, auth http.Header, repo string, noprecommit, nocommit bool, taskStore *TaskStore) error {
	act, err := api.ActorAddress(ctx)
	if err != nil {
		return err
	}
	ssize, err := api.ActorSectorSize(ctx, act)
	if err != nil {
		return err
	}

	sb, err := sectorbuilder.NewStandalone(&sectorbuilder.Config{
		SectorSize:    ssize,
		Miner:         act,
		WorkerThreads: 1,
		Dir:           repo,
	})
	if err != nil {
		return err
	}

	if err := paramfetch.GetParams(build.ParametersJson, ssize); err != nil {
		return xerrors.Errorf("get params: %w", err)
	}

	w := &worker{
		api:           api,
		minerEndpoint: endpoint,
		auth:          auth,
		repo:          repo,
		sb:            sb,
	}

	myIP, err := getMyIP()
	if err != nil {
		return err
	}

	cfg := sectorbuilder.WorkerCfg{
		NoPreCommit: noprecommit,
		NoCommit:    nocommit,
		Directory:   repo,
		IPAddress:   myIP.String(),
	}

	storedTasks, err := taskStore.ListTasks()
	if err != nil {
		return err
	}
	for _, task := range storedTasks {
		finished, err := api.WorkerResume(ctx, task.WorkerTask, task.SealRes, cfg)
		if err != nil {
			return err
		} else if !finished {
			log.Infof("Resumed done task: sector %d, action %d", task.WorkerTask.SectorID, task.WorkerTask.Type)
		} else {
			log.Infof("Finished done task: sector %d", task.WorkerTask.SectorID)
			err = taskStore.Delete(task.WorkerTask.SectorID)
			if err != nil {
				return err
			}
		}
	}

	tasks, err := api.WorkerQueue(ctx, cfg)
	if err != nil {
		return err
	}

loop:
	for {
		log.Infof("Waiting for new task")

		select {
		case task := <-tasks:

			log.Infof("New task: %d, sector %d, action: %d", task.TaskID, task.SectorID, task.Type)

			res := w.processTask(ctx, task)

			log.Infof("Task %d done, err: %+v", task.TaskID, res.GoErr)

			err = taskStore.PutTask(Task{
				WorkerTask: task,
				SealRes:    res,
			})
			if err != nil {
				log.Errorf("Store task: %+v", err)
			}

			if err := api.WorkerDone(ctx, task.TaskID, res); err != nil {
				log.Error(err)
				return err
			}
		case <-ctx.Done():
			break loop
		}
	}

	log.Warn("acceptJobs exit")
	return nil
}

func (w *worker) processTask(ctx context.Context, task sectorbuilder.WorkerTask) sectorbuilder.SealRes {
	switch task.Type {
	case sectorbuilder.WorkerPreCommit:
	case sectorbuilder.WorkerCommit:
	default:
		return errRes(xerrors.Errorf("unknown task type %d", task.Type))
	}

	/* if err := w.fetchSector(task.SectorID, task.Type); err != nil {
		return errRes(xerrors.Errorf("fetching sector: %w", err))
	} */

	log.Infof("Data fetched, starting computation")

	var res sectorbuilder.SealRes

	switch task.Type {
	case sectorbuilder.WorkerPreCommit:
		rspco, err := w.sb.SealPreCommit(ctx, task.SectorID, task.SealTicket, task.Pieces)
		if err != nil {
			return errRes(xerrors.Errorf("precomitting: %w", err))
		}
		res.Rspco = rspco.ToJson()

		/* if err := w.push("sealed", task.SectorID); err != nil {
			return errRes(xerrors.Errorf("pushing precommited data: %w", err))
		}

		if err := w.push("cache", task.SectorID); err != nil {
			return errRes(xerrors.Errorf("pushing precommited data: %w", err))
		}

		if err := w.remove("staging", task.SectorID); err != nil {
			return errRes(xerrors.Errorf("cleaning up staged sector: %w", err))
		} */
	case sectorbuilder.WorkerCommit:
		proof, _, err := w.sb.SealCommit(ctx, task.SectorID, task.SealTicket, task.SealSeed, task.Pieces, task.Rspco)
		if err != nil {
			return errRes(xerrors.Errorf("comitting: %w", err))
		}

		res.Proof = proof

		/* if err := w.push("cache", task.SectorID); err != nil {
			return errRes(xerrors.Errorf("pushing precommited data: %w", err))
		}

		if err := w.remove("sealed", task.SectorID); err != nil {
			return errRes(xerrors.Errorf("cleaning up sealed sector: %w", err))
		} */
	}

	return res
}

func errRes(err error) sectorbuilder.SealRes {
	return sectorbuilder.SealRes{Err: err.Error(), GoErr: err}
}

func getMyIP() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil && strings.HasPrefix(ipnet.IP.String(), "192.168.") {
				return ipnet.IP, nil
			}
		}
	}
	return nil, errors.New("my ip not found")
}

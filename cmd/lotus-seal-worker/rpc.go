package main

import (
	"context"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/build"
	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	"github.com/filecoin-project/lotus/extern/sector-storage/snark"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
)


type worker struct {
	*sectorstorage.LocalWorker

	localStore *stores.Local
	ls         stores.LocalStorage

	snarkctl *snark.SnarkCtl
	disabled int64
}

func (w *worker) AddSnark(ctx context.Context, snarkUrl string) error {
	if err := w.SetSnark(func(sc *snark.SnarkInfo) {
		sc.SnarkUrls = append(sc.SnarkUrls, snark.SnarkUrl{
			Path:  snarkUrl,
			State: snark.SnarkFree,
		})
	}); err != nil {
		return xerrors.Errorf("set snark config: %w", err)
	}
	return nil
}

func (w *worker) RemoveSnark(ctx context.Context, snarkUrl string) error {
	if err := w.SetSnark(func(sc *snark.SnarkInfo) {
		for i, snark := range sc.SnarkUrls {
			if snark.Path == snarkUrl {
				sc.SnarkUrls = append(sc.SnarkUrls[:i], sc.SnarkUrls[i+1:]...)
				break
			}
		}
	}); err != nil {
		return xerrors.Errorf("remove snark config: %w", err)
	}
	return nil
}
func (w *worker)SetSnark(c func(*snark.SnarkInfo)) error {
	w.snarkctl.SnarkLk.Lock()
	defer w.snarkctl.SnarkLk.Unlock()

	sc, err := w.snarkctl.GetSnark()
	if err != nil {
		return xerrors.Errorf("get storage: %w", err)
	}

	c(sc)

	return snark.WriteSnarkFile(w.snarkctl.SnarkConfig, *sc)
}

func (w *worker) Version(context.Context) (build.Version, error) {
	return build.WorkerAPIVersion, nil
}

func (w *worker) StorageAddLocal(ctx context.Context, path string) error {
	path, err := homedir.Expand(path)
	if err != nil {
		return xerrors.Errorf("expanding local path: %w", err)
	}

	if err := w.localStore.OpenPath(ctx, path); err != nil {
		return xerrors.Errorf("opening local path: %w", err)
	}

	if err := w.ls.SetStorage(func(sc *stores.StorageConfig) {
		sc.StoragePaths = append(sc.StoragePaths, stores.LocalPath{Path: path})
	}); err != nil {
		return xerrors.Errorf("get storage config: %w", err)
	}

	return nil
}

func (w *worker) SetEnabled(ctx context.Context, enabled bool) error {
	disabled := int64(1)
	if enabled {
		disabled = 0
	}
	atomic.StoreInt64(&w.disabled, disabled)
	return nil
}

func (w *worker) Enabled(ctx context.Context) (bool, error) {
	return atomic.LoadInt64(&w.disabled) == 0, nil
}

func (w *worker) WaitQuiet(ctx context.Context) error {
	w.LocalWorker.WaitQuiet() // uses WaitGroup under the hood so no ctx :/
	return nil
}

func (w *worker) ProcessSession(ctx context.Context) (uuid.UUID, error) {
	return w.LocalWorker.Session(ctx)
}

func (w *worker) Session(ctx context.Context) (uuid.UUID, error) {
	if atomic.LoadInt64(&w.disabled) == 1 {
		return uuid.UUID{}, xerrors.Errorf("worker disabled")
	}

	return w.LocalWorker.Session(ctx)
}

var _ storiface.WorkerCalls = &worker{}

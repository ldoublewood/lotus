package storage

import (
	"context"
	"errors"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-sectorbuilder"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/gen"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealing"
)

var log = logging.Logger("storageminer")

type Miner struct {
	api   storageMinerApi
	h     host.Host
	sb    sectorbuilder.Interface
	ds    datastore.Batching
	tktFn sealing.TicketFn

	maddr  address.Address
	worker address.Address

	sealing *sealing.Sealing

	stop    chan struct{}
	stopped chan struct{}
}

type PledgeSectorMode string

const (
	PledgeSectorModeClose  PledgeSectorMode = "close"
	PledgeSectorModeAll    PledgeSectorMode = "all"
	PledgeSectorModeRemote PledgeSectorMode = "remote"
	PledgeSectorModeLocal  PledgeSectorMode = "local"
)

type storageMinerApi interface {
	// Call a read only method on actors (no interaction with the chain required)
	StateCall(ctx context.Context, msg *types.Message, ts *types.TipSet) (*types.MessageReceipt, error)
	StateMinerWorker(context.Context, address.Address, *types.TipSet) (address.Address, error)
	StateMinerElectionPeriodStart(ctx context.Context, actor address.Address, ts *types.TipSet) (uint64, error)
	StateMinerSectors(context.Context, address.Address, *types.TipSet) ([]*api.ChainSectorInfo, error)
	StateMinerProvingSet(context.Context, address.Address, *types.TipSet) ([]*api.ChainSectorInfo, error)
	StateMinerSectorSize(context.Context, address.Address, *types.TipSet) (uint64, error)
	StateWaitMsg(context.Context, cid.Cid) (*api.MsgWait, error) // TODO: removeme eventually
	StateGetActor(ctx context.Context, actor address.Address, ts *types.TipSet) (*types.Actor, error)
	StateGetReceipt(context.Context, cid.Cid, *types.TipSet) (*types.MessageReceipt, error)

	MpoolPushMessage(context.Context, *types.Message) (*types.SignedMessage, error)

	ChainHead(context.Context) (*types.TipSet, error)
	ChainNotify(context.Context) (<-chan []*store.HeadChange, error)
	ChainGetRandomness(context.Context, types.TipSetKey, int64) ([]byte, error)
	ChainGetTipSetByHeight(context.Context, uint64, *types.TipSet) (*types.TipSet, error)
	ChainGetBlockMessages(context.Context, cid.Cid) (*api.BlockMessages, error)

	WalletSign(context.Context, address.Address, []byte) (*types.Signature, error)
	WalletBalance(context.Context, address.Address) (types.BigInt, error)
	WalletHas(context.Context, address.Address) (bool, error)
}

func NewMiner(api storageMinerApi, addr address.Address, h host.Host, ds datastore.Batching, sb sectorbuilder.Interface, tktFn sealing.TicketFn) (*Miner, error) {
	m := &Miner{
		api:   api,
		h:     h,
		sb:    sb,
		ds:    ds,
		tktFn: tktFn,

		maddr: addr,

		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	return m, nil
}

func (m *Miner) Run(ctx context.Context) error {
	if err := m.runPreflightChecks(ctx); err != nil {
		return xerrors.Errorf("miner preflight checks failed: %w", err)
	}

	fps := &fpostScheduler{
		api: m.api,
		sb:  m.sb,

		actor:  m.maddr,
		worker: m.worker,
		miner:  m,
	}

	go fps.run(ctx)

	evts := events.NewEvents(ctx, m.api)
	m.sealing = sealing.New(m.api, evts, m.maddr, m.worker, m.ds, m.sb, m.tktFn)

	go m.sealing.Run(ctx)

	return nil
}

func (m *Miner) Stop(ctx context.Context) error {
	defer m.sealing.Stop(ctx)

	close(m.stop)
	select {
	case <-m.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Miner) runPreflightChecks(ctx context.Context) error {
	worker, err := m.api.StateMinerWorker(ctx, m.maddr, nil)
	if err != nil {
		return err
	}

	m.worker = worker

	has, err := m.api.WalletHas(ctx, worker)
	if err != nil {
		return xerrors.Errorf("failed to check wallet for worker key: %w", err)
	}

	if !has {
		return errors.New("key for worker not found in local wallet")
	}

	log.Infof("starting up miner %s, worker addr %s", m.maddr, m.worker)
	return nil
}

type SectorBuilderEpp struct {
	sb sectorbuilder.Interface
	miner *Miner
}

func NewElectionPoStProver(sb sectorbuilder.Interface, miner *Miner) *SectorBuilderEpp {
	return &SectorBuilderEpp{sb, miner}
}

var _ gen.ElectionPoStProver = (*SectorBuilderEpp)(nil)

func (epp *SectorBuilderEpp) GenerateCandidates(ctx context.Context, ssi sectorbuilder.SortedPublicSectorInfo, rand []byte) ([]sectorbuilder.EPostCandidate, error) {
	start := time.Now()
	var faults []uint64 // TODO

	if epp.miner != nil {
		err := epp.miner.filWorkerDirForSectors(ssi)
		if err != nil {
			return nil, err
		}
	}

	var randbuf [32]byte
	copy(randbuf[:], rand)
	cds, err := epp.sb.GenerateEPostCandidates(ssi, randbuf, faults)
	if err != nil {
		return nil, err
	}
	log.Infof("Generate candidates took %s", time.Since(start))
	return cds, nil
}

func (epp *SectorBuilderEpp) ComputeProof(ctx context.Context, ssi sectorbuilder.SortedPublicSectorInfo, rand []byte, winners []sectorbuilder.EPostCandidate) ([]byte, error) {
	if build.InsecurePoStValidation {
		log.Warn("Generating fake EPost proof! You should only see this while running tests!")
		return []byte("valid proof"), nil
	}
	start := time.Now()

	if epp.miner != nil {
		err := epp.miner.filWorkerDirForSectors(ssi)
		if err != nil {
			return nil, err
		}
	}

	proof, err := epp.sb.ComputeElectionPoSt(ssi, rand, winners)
	if err != nil {
		return nil, err
	}
	log.Infof("ComputeElectionPost took %s", time.Since(start))
	return proof, nil
}

func (m *Miner) filWorkerDirForSectors(ssi sectorbuilder.SortedPublicSectorInfo) error {
	for i, s := range ssi.Values() {
		sector, err := m.GetSectorInfo(s.SectorID)
		if err != nil {
			return err
		}
		ssi.Values()[i].WorkerDir = sector.WorkerDir
	}
	return nil
}

func (m *Miner) WorkerResume(ctx context.Context, task sectorbuilder.WorkerTask, res sectorbuilder.SealRes, cfg sectorbuilder.WorkerCfg) (bool, error) {
	sector, err := m.GetSectorInfo(task.SectorID)
	if err != nil {
		return false, err
	}
	switch sector.State {
	case api.Proving:
		return true, err

	case api.PreCommitting:
		return false, err
	case api.PreCommitted:
		return false, err
	case api.Committing:
		return false, err
	case api.CommitWait:
		return false, err

	case api.SealFailed:
		fallthrough
	case api.PreCommitFailed:
		log.Infof("Resume sector %d from %s", sector.SectorID, api.SectorStates[api.PreCommitFailed])
		m.handleSectorUpdate(ctx, sector, func (ctx context.Context, sector SectorInfo) *sectorUpdate {
			return sector.upd().to(api.PreCommitting).state(func(info *SectorInfo) {
				info.CommD = task.Rspco.CommD[:]
				info.CommR = task.Rspco.CommR[:]
				info.Ticket = SealTicket{
					BlockHeight: task.SealTicket.BlockHeight,
					TicketBytes: task.SealTicket.TicketBytes[:],
				}
			})
		})
		return false, err

	case api.SealCommitFailed:
		if task.Type == sectorbuilder.WorkerCommit {
			log.Infof("Resume sector %d from %s", sector.SectorID, api.SectorStates[api.SealCommitFailed])
			workerDir, err := m.sb.GetPath("workers", cfg.IPAddress)
			if err != nil {
				return false, err
			}
			sector.Proof = res.Proof
			sector.WorkerDir = workerDir
			m.handleSectorUpdate(ctx, sector, m.handleCommitting)
		} else {
			// TODO: resume commit task
		}
		return false, err

	case api.CommitFailed:
		log.Infof("Resume sector %d from %s", sector.SectorID, api.SectorStates[api.CommitFailed])
		m.handleSectorUpdate(ctx, sector, m.handleCommitting)
		return false, err

	case api.FailedUnrecoverable:
		// TODO: resume?
	}

	return true, err // don't know how to handle, so treat it as finished
}

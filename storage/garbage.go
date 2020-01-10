package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"math"
	sectorbuilder "github.com/filecoin-project/go-sectorbuilder"
	"golang.org/x/xerrors"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

//var commP []byte
//var lock sync.Mutex

func (m *Miner) pledgeSector(ctx context.Context, sectorID uint64, existingPieceSizes []uint64, sizes ...uint64) ([]Piece, error) {
	if len(sizes) == 0 {
		return nil, nil
	}

	deals := make([]actors.StorageDealProposal, len(sizes))
	for i, size := range sizes {
		log.Infof("RateLimit begin %d", sectorID)
		release := m.sb.RateLimit()
		log.Infof("RateLimit end %d", sectorID)
		//lock.Lock()
		//if commP == nil {
		//	ccommP, err := sectorbuilder.GeneratePieceCommitment(io.LimitReader(rand.New(rand.NewSource(42)), int64(size)), size)
		//	if err != nil {
		//		panic(err)
		//	}
		//	commP = make([]byte, sectorbuilder.CommLen)
		//	copy(commP, ccommP[:])
		//}
		//lock.Unlock()
		//log.Infof("GeneratePieceCommitment %d, %q", sectorID, commP)
		commP, err := hex.DecodeString("fd2fc3c8f13169111766c62c629262752b2be468f531cfc88c0b47d1ac13c62e")

		release()
		if err != nil {
			panic(err)
		}

		sdp := actors.StorageDealProposal{
			PieceRef:             commP[:],
			PieceSize:            size,
			Client:               m.worker,
			Provider:             m.maddr,
			ProposalExpiration:   math.MaxUint64,
			Duration:             math.MaxUint64 / 2, // /2 because overflows
			StoragePricePerEpoch: types.NewInt(0),
			StorageCollateral:    types.NewInt(0),
			ProposerSignature:    nil,
		}

		if err := api.SignWith(ctx, m.api.WalletSign, m.worker, &sdp); err != nil {
			return nil, xerrors.Errorf("signing storage deal failed: ", err)
		}

		deals[i] = sdp
	}

	params, aerr := actors.SerializeParams(&actors.PublishStorageDealsParams{
		Deals: deals,
	})
	if aerr != nil {
		return nil, xerrors.Errorf("serializing PublishStorageDeals params failed: ", aerr)
	}
	log.Infof("MpoolPushMessage %d", sectorID)

	smsg, err := m.api.MpoolPushMessage(ctx, &types.Message{
		To:       actors.StorageMarketAddress,
		From:     m.worker,
		Value:    types.NewInt(0),
		GasPrice: types.NewInt(0),
		GasLimit: types.NewInt(1000000),
		Method:   actors.SMAMethods.PublishStorageDeals,
		Params:   params,
	})
	if err != nil {
		return nil, err
	}
	r, err := m.api.StateWaitMsg(ctx, smsg.Cid())
	log.Infof("StateWaitMsg %d", sectorID)

	if err != nil {
		return nil, err
	}
	if r.Receipt.ExitCode != 0 {
		log.Error(xerrors.Errorf("publishing deal failed: exit %d", r.Receipt.ExitCode))
	}
	var resp actors.PublishStorageDealResponse
	if err := resp.UnmarshalCBOR(bytes.NewReader(r.Receipt.Return)); err != nil {
		return nil, err
	}
	if len(resp.DealIDs) != len(sizes) {
		return nil, xerrors.New("got unexpected number of DealIDs from PublishStorageDeals")
	}

	out := make([]Piece, len(sizes))

	for i, size := range sizes {
		//log.Infof("AddPiece begin %d", sectorID)
		//ppi, err := m.sb.AddPiece(size, sectorID, io.LimitReader(rand.New(rand.NewSource(42)), int64(size)), existingPieceSizes)
		//if err != nil {
		//	return nil, err
		//}
		//log.Infof("AddPiece end %d", sectorID)

		existingPieceSizes = append(existingPieceSizes, size)

		//out[i] = Piece{
		//	DealID: resp.DealIDs[i],
		//	Size:   ppi.Size,
		//	CommP:  ppi.CommP[:],
		//}


		commP, err := hex.DecodeString("fd2fc3c8f13169111766c62c629262752b2be468f531cfc88c0b47d1ac13c62e")

		if err != nil {
			panic(err)
		}
		out[i] = Piece{
			DealID: resp.DealIDs[i],
			Size:   34091302912,
			CommP:  commP,
		}
	}

	return out, nil
}

func (m *Miner) PledgeSector(ctx context.Context) error {
	size := sectorbuilder.UserBytesForSectorSize(m.sb.SectorSize())

	sid, err := m.sb.AcquireSectorId()
	if err != nil {
		log.Errorf("%+v", err)
		return err
	}
	log.Infof("acquir %d", sid)

	pieces, err := m.pledgeSector(ctx, sid, []uint64{}, size)
	if err != nil {
		log.Errorf("%+v", err)
		return err
	}

	log.Infof("pledgeSector %d", sid)

	if err := m.newSector(ctx, sid, pieces[0].DealID, pieces[0].ppi()); err != nil {
		log.Errorf("%+v", err)
		return err
	}
	log.Infof("newSector %d", sid)

	return nil
}

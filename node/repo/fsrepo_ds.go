package repo

import (
	"os"
	"path/filepath"

	"github.com/ipfs/go-datastore"
	"golang.org/x/xerrors"

	dgbadger "github.com/dgraph-io/badger/v2"
	badger "github.com/ipfs/go-ds-badger2"
	levelds "github.com/ipfs/go-ds-leveldb"
	measure "github.com/ipfs/go-ds-measure"
	ldbopts "github.com/syndtr/goleveldb/leveldb/opt"
)

type dsCtor func(path string) (datastore.Batching, error)

var fsDatastores = map[string]dsCtor{
	"chain":    chainBadgerDs,
	"metadata": levelDs,

	// Those need to be fast for large writes... but also need a really good GC :c
	"staging": badgerDs, // miner specific

	"client": badgerDs, // client specific
}

func chainBadgerDs(path string) (datastore.Batching, error) {
	opts := badger.DefaultOptions
	opts.GcInterval = 0 // disable GC for chain datastore

	opts.Options = dgbadger.DefaultOptions("").WithTruncate(true).
		WithValueThreshold(1 << 10)
	if os.Getenv("RUN_POST_ONLY") == "_yes_" {
		opts.BypassLockGuard = true
	}
	return badger.NewDatastore(path, &opts)
}

func badgerDs(path string) (datastore.Batching, error) {
	opts := badger.DefaultOptions
	opts.Options = dgbadger.DefaultOptions("").WithTruncate(true).
		WithValueThreshold(1 << 10)

	if os.Getenv("RUN_POST_ONLY") == "_yes_" {
		opts.BypassLockGuard = true
	}
	return badger.NewDatastore(path, &opts)
}

func levelDs(path string) (datastore.Batching, error) {
	readOnly := os.Getenv("RUN_POST_ONLY") == "_yes_"
	op := &levelds.Options{
			Compression: ldbopts.NoCompression,
			NoSync:      false,
			Strict:      ldbopts.StrictAll,
		    ReadOnly:     readOnly,
	}

	return levelds.NewDatastore(path, op)
}

func (fsr *fsLockedRepo) openDatastores() (map[string]datastore.Batching, error) {
	if err := os.MkdirAll(fsr.join(fsDatastore), 0755); err != nil {
		return nil, xerrors.Errorf("mkdir %s: %w", fsr.join(fsDatastore), err)
	}

	out := map[string]datastore.Batching{}

	for p, ctor := range fsDatastores {
		prefix := datastore.NewKey(p)

		// TODO: optimization: don't init datastores we don't need
		ds, err := ctor(fsr.join(filepath.Join(fsDatastore, p)))
		if err != nil {
			return nil, xerrors.Errorf("opening datastore %s: %w", prefix, err)
		}

		ds = measure.New("fsrepo."+p, ds)

		out[datastore.NewKey(p).String()] = ds
	}

	return out, nil
}

func (fsr *fsLockedRepo) Datastore(ns string) (datastore.Batching, error) {
	fsr.dsOnce.Do(func() {
		fsr.ds, fsr.dsErr = fsr.openDatastores()
	})

	if fsr.dsErr != nil {
		return nil, fsr.dsErr
	}
	ds, ok := fsr.ds[ns]
	if ok {
		return ds, nil
	}
	return nil, xerrors.Errorf("no such datastore: %s", ns)
}

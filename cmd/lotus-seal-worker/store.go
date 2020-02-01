package main

import (
	"encoding/json"
	"fmt"
	"path"
	"time"

	"github.com/filecoin-project/go-sectorbuilder"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	badger "github.com/ipfs/go-ds-badger2"
)

type Task struct {
	WorkerTask sectorbuilder.WorkerTask
	SealRes sectorbuilder.SealRes
}

type TaskStore struct {
	*badger.Datastore
}

func NewTaskStore(p string) (*TaskStore, error) {
	opts := badger.DefaultOptions
	opts.Truncate = true
	ds, err := badger.NewDatastore(path.Join(p, "tasks"), &opts)
	if err != nil {
		return nil, err
	}
	return &TaskStore{ds}, nil
}

func (s *TaskStore) PutTask(task Task) error {
	value, err := json.Marshal(&task)
	if err != nil {
		return err
	}
	return s.PutWithTTL(ToKey(task.WorkerTask.SectorID), value, time.Hour*24*3)
}

func (s *TaskStore) GetTask(sectorID uint64) (Task, error) {
	value, err := s.Get(ToKey(sectorID))
	if err != nil {
		return Task{}, err
	}
	var task Task
	err = json.Unmarshal(value, &task)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *TaskStore) ListTasks() ([]Task, error) {
	res, err := s.Query(query.Query{})
	if err != nil {
		return nil, err
	}
	defer res.Close()

	var tasks []Task

	for {
		res, ok := res.NextSync()
		if !ok {
			break
		}
		if res.Error != nil {
			return nil, res.Error
		}

		var task Task
		err = json.Unmarshal(res.Value, &task)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

func (s *TaskStore) Delete(sectorID uint64) error {
	return s.Datastore.Delete(ToKey(sectorID))
}

func ToKey(k interface{}) datastore.Key {
	switch t := k.(type) {
	case uint64:
		return datastore.NewKey(fmt.Sprint(t))
	case fmt.Stringer:
		return datastore.NewKey(t.String())
	default:
		panic("unexpected key type")
	}
}

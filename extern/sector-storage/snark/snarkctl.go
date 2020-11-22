package snark

import (
	"encoding/json"
	"github.com/prometheus/common/log"
	"golang.org/x/xerrors"
	"io"
	"io/ioutil"
	"os"
	"sync"
)
type State string

const (
	SnarkFree State = "free"
	SnarkBusy State = "busy"
)
type SnarkCtl struct {
	//snarkCx []string\
	info        *SnarkInfo
	CxSnark     bool
	SnarkLk     sync.Mutex
	snarkConfig string
}
type SnarkUrl struct {
	Path  string
	State State
}
type SnarkInfo struct {
	SnarkUrls []SnarkUrl
}

func NewSnarkCtl() *SnarkCtl {
	return &SnarkCtl{
		CxSnark:     os.Getenv("USE_CX_SNARK") == "_yes_",
		SnarkLk:     sync.Mutex{},
		snarkConfig: "/etc/CxSnark.json",
	}
}

func (ctl *SnarkCtl)Load() error {
	info, err := ctl.GetSnark()
	if err != nil {
		return xerrors.Errorf("get storage: %w", err)
	}

	ctl.info = info
	return nil
}

func (ctl *SnarkCtl) GetSnark() (*SnarkInfo, error) {
	return SnarkFromFile(ctl.snarkConfig)
}
func (ctl *SnarkCtl) ObtainSnark() (string, error) {
	for _, url := range ctl.info.SnarkUrls {
		if url.State == SnarkFree {
			url.State = SnarkBusy
			return url.Path, nil
		}
	}
	return "", xerrors.Errorf("no available snark url")
}
func (ctl *SnarkCtl) FreeSnark(urlToFree string) error {
	for _, url := range ctl.info.SnarkUrls {
		if url.Path == urlToFree {
			if url.State != SnarkBusy {
				log.Warnf("the target url is not in busy state: %s, url: %s", url.State, url.Path)
			}
			url.State = SnarkFree
			return nil
		}
	}
	return xerrors.Errorf("snark url not found: %s", urlToFree)
}

//func (ctl *SnarkCtl) GetFreeSnark() (*SnarkInfo, error) {
//	return SnarkFromFile(ctl.snarkConfig)
//}
func SnarkFromFile(path string) (*SnarkInfo, error) {
	file, err := os.Open(path)
	switch {
	case os.IsNotExist(err):
		return nil, xerrors.Errorf("couldn't load snark config: %w", err)
	case err != nil:
		return nil, err
	}

	defer file.Close() //nolint:errcheck // The file is RO
	return SnarkFromReader(file)
}

func SnarkFromReader(reader io.Reader) (*SnarkInfo, error) {
	var cfg SnarkInfo
	err := json.NewDecoder(reader).Decode(&cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
func WriteSnarkFile(path string, info SnarkInfo) error {
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return xerrors.Errorf("marshaling snark info: %w", err)
	}

	if err := ioutil.WriteFile(path, b, 0644); err != nil {
		return xerrors.Errorf("persisting snark info (%s): %w", path, err)
	}

	return nil
}



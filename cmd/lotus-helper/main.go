package main

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/address"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/jsonrpc"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/mitchellh/go-homedir"
	"golang.org/x/xerrors"
	"gopkg.in/urfave/cli.v2"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const FlagStorageRepo = "storagerepo"

const ProxyAddr = "proxyaddr"
const ProxyFetcherAddr = "proxyfetcheraddr"

var log = logging.Logger("main")

func main() {
	logging.SetLogLevel("*", "INFO")

	log.Info("Starting lotus starter")

	local := []*cli.Command{
		runCmd,
	}

	app := &cli.App{
		Name:    "lotus-helper",
		Usage:   "Devnet helper",
		Version: build.Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repo",
				EnvVars: []string{"LOTUS_PATH"},
				Value:   "~/.lotus", // TODO: Consider XDG_DATA_HOME
			},
		},

		Commands: local,
	}

	if err := app.Run(os.Args); err != nil {
		log.Warn(err)
		return
	}
}


var runCmd = &cli.Command{
	Name:  "run",
	Usage: "Start lotus helper",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "createminer",
			Value:   true,
			EnvVars: []string{"CREATE_MINER"},
		},
		&cli.StringFlag{
			Name:    FlagStorageRepo,
			EnvVars: []string{"LOTUS_STORAGE_PATH"},
			Value:   "~/.lotusstorage",
		},
		&cli.StringFlag{
			Name:    ProxyAddr,
			EnvVars: []string{"PROXY_ADDR"},
			Usage: "proxy address to create miner by web",
			Value:   "",
		},
		&cli.StringFlag{
			Name:    ProxyFetcherAddr,
			EnvVars: []string{"PROXY_FETCHER_ADDR"},
			Usage: "proxy fetcher server to create miner by web, disabled when ProxyAddr is set",
			Value:   "proxy-fetcher",
		},
		&cli.StringFlag{
			Name:  "listen",
			Value: "127.0.0.1:8899",
		},
	},
	Action: func(cctx *cli.Context) error {
	   var err error
		var fnapi api.FullNode
		var fncloser jsonrpc.ClientCloser
		fnapi, fncloser, err = lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer fncloser()
		ctx := lcli.ReqContext(cctx)

		go func() {
			<-ctx.Done()
			os.Exit(0)
		}()

		v, err := fnapi.Version(ctx)
		if err != nil {
			return err
		}

		log.Infof("Remote version: %s", v.Version)

		h := &handler{
			ctx: ctx,
			api: fnapi,
			cctx: cctx,
		}

		createminerFlag := cctx.Bool("createminer")
		if createminerFlag {
			repoPath := cctx.String(FlagStorageRepo)
			err = h.createMiner(fnapi, repoPath)
			if err != nil {
				return err
			}
		}

		http.HandleFunc("/help", h.help)

		log.Info("Starting to listen")


		return http.ListenAndServe(cctx.String("listen"), nil)

	},
}


type handler struct {
	ctx context.Context
	api api.FullNode
	cctx *cli.Context

}


func (h *handler) help(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte("OK"))
	return
}

//proxy format: ip:port
func doCreateMinerByWeb(address string, proxy string) (actor string, err error) {
	var urlproxy *url.URL
	var client *http.Client

	if len(proxy) != 0 {
		log.Infof("using proxy: %s", proxy)
		urli := url.URL{}
		urlproxy, err = urli.Parse("http://" +proxy)
		if err != nil {
			return "", err
		}
		client = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(urlproxy),
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	} else {
		client = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

	}

	url := "https://lotus-faucet.kittyhawk.wtf/mkminer"
	data := "address=" + address + "&sectorSize=1073741824"


	var request *http.Request
	request, err = http.NewRequest("POST", url,  strings.NewReader(data))
	if err != nil {
		return "", err
	}

	request.Header.Add("Content-Type", "application/x-www-form-urlencoded")


	var response *http.Response
	response, err = client.Do(request)
	if err != nil {
		return "", err
	}
	var body []byte


	var reader io.ReadCloser
	switch response.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(response.Body)
		defer reader.Close()
	default:
		reader = response.Body
	}

	body, err = ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}

	log.Infof("response: %s", string(body))

	if response.StatusCode == 429 {
		msg := "too many request"
		log.Warn(msg)
		return "", errors.New(msg)
	} else if response.StatusCode != 303 {
		msg := "unknown status code"
		log.Warnf("%s: %d", msg, response.StatusCode)
		return "", errors.New(msg)
	}
	//303 is the correct response
	header := response.Header
	location := header.Get("Location")
	if len(location) == 0 {
		msg := "location is not found in Header"
		log.Warn(msg)
		return "",errors.New(msg)
	}
	// Location: /wait.html?f=bafy2bzaceds2s6scrmycgy7jlfubqnb3jxbiowsopdgjq35ignro4ecd2dv7u&m=bafy2bzaceabasestsz3nuxjxf3q75oeu2wbilqerh5jjsajtl7jnhuqvk43fw&o=t3rfta3odxzvfx6svhsfz6xohw52523co4xs5kebmhuzsxiz5cbnyztla2cb3suvbwupea5vpxjay4ga44d3sa
	log.Info("location: %s", location)
	before := "/wait.html?f="
	start := strings.Index(location, before)
	if start == -1 {
		log.Warnf("should started with %s", before)
		return "", errors.New("error location format")
	}

	after := "&m="
	end := strings.Index(location, after)

	f := location[start + len(before):end]
	var addr string
	addr, err = msgwait(f, client)
	if err != nil {
		return "", err
	}
	return addr, nil

}


func storageMinerInit(ctx context.Context, cctx *cli.Context, fnapi api.FullNode, r repo.Repo, act string) error {
	lr, err := r.Lock(repo.StorageMiner)
	if err != nil {
		return err
	}
	defer lr.Close()

	log.Info("Initializing libp2p identity")

	p2pSk, err := makeHostKey(lr)
	if err != nil {
		return xerrors.Errorf("make host key: %w", err)
	}

	peerid, err := peer.IDFromPrivateKey(p2pSk)
	if err != nil {
		return xerrors.Errorf("peer ID from private key: %w", err)
	}

	mds, err := lr.Datastore("/metadata")
	if err != nil {
		return err
	}

	var addr address.Address
	//if act := cctx.String("actor"); act != "" {
		a, err := address.NewFromString(act)
		if err != nil {
			return xerrors.Errorf("failed parsing actor flag value (%q): %w", act, err)
		}

		if err := configureStorageMiner(ctx, fnapi, a, peerid); err != nil {
			return xerrors.Errorf("failed to configure storage miner: %w", err)
		}

		addr = a
	//}

	log.Infof("Created new storage miner: %s", addr)
	if err := mds.Put(datastore.NewKey("miner-address"), addr.Bytes()); err != nil {
		return err
	}

	return nil
}


func (h *handler) createMiner(fnapi api.FullNode, repoPath string) (err error) {
	const Interval = 10
	const MaxTry = 100
	var addrs []address.Address
	var actor string
	addrs, err = fnapi.WalletList(h.ctx)
	if err != nil {
		return err
	}

	if len(addrs) != 0 {
		log.Info("already has wallet address, so do nothing")
		return nil
	}
	var nk address.Address
	nk, err = fnapi.WalletNew(h.ctx, "bls")
	strAddress := nk.String()
	log.Infof("new wallet address: %s", strAddress)

	if err != nil {
		return err
	}
	log.Infof("wallet address created, %s", strAddress)

	//owner is just used for hint in proxy lock
	owner := strAddress[:5]
	var proxy string
	proxy, err = getProxy(h.cctx, owner)
	if err != nil {
		return err
	}
	for i := 1; i < MaxTry; i++ {
		actor, err = doCreateMinerByWeb(strAddress, proxy)
		if err == nil {
			log.Info("create miner by web succeed")
			break
		}

		log.Warnf("create miner by web fail", err.Error())
		time.Sleep(Interval * time.Second)
	}

	log.Infof("actor is: %s", actor)
	//repoPath := h.ctx.String(FlagStorageRepo)
	r, err := repo.NewFS(repoPath)
	if err != nil {
		return err
	}

	ok, err := r.Exists()
	if err != nil {
		return err
	}
	if ok {
		return xerrors.Errorf("repo at '%s' is already initialized", h.cctx.String(FlagStorageRepo))
	}

	log.Info("Checking full node version")

	v, err := fnapi.Version(h.ctx)
	if err != nil {
		return err
	}

	if v.APIVersion&build.MinorMask != build.APIVersion&build.MinorMask {
		return xerrors.Errorf("Remote API version didn't match (local %x, remote %x)", build.APIVersion, v.APIVersion)
	}

	log.Info("Sleeping 60 second before initializing repo")

	time.Sleep(60 * time.Second)

	log.Info("Initializing repo")

	if err := r.Init(repo.StorageMiner); err != nil {
		return err
	}

	if err := storageMinerInit(h.ctx, h.cctx, fnapi, r, actor); err != nil {
		log.Errorf("Failed to initialize lotus-storage-miner: %+v", err)
		path, err := homedir.Expand(repoPath)
		if err != nil {
			return err
		}
		log.Infof("Cleaning up %s after attempt...", path)
		if err := os.RemoveAll(path); err != nil {
			log.Errorf("Failed to clean up failed storage repo: %s", err)
		}
		return xerrors.Errorf("Storage-miner init failed")
	}


	return nil


}

type WaitResponse struct {
	Addr string `json:"addr"`
}

func msgwait(cid string, client *http.Client) (string, error) {
	var err error
	url := "https://lotus-faucet.kittyhawk.wtf/msgwaitaddr?cid=" + cid
	//curl -vvv "https://lotus-faucet.kittyhawk.wtf/msgwaitaddr?cid=bafy2bzaceds2s6scrmycgy7jlfubqnb3jxbiowsopdgjq35ignro4ecd2dv7u"

	var request *http.Request
	request, err = http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	var response *http.Response
	response, err = client.Do(request)
	if err != nil {
		return "", err
	}
	var body []byte
	body, err = ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	//{"addr": "t01607"}
	log.Infof("reponse: %s", string(body))
	var r = WaitResponse{}
	err = json.Unmarshal(body,&r)
	if err != nil {
		return "", nil
	}
	addr := r.Addr
	log.Infof("addr is: %s", addr)
	return addr, nil

}

func getProxy(cctx *cli.Context, owner string) (string, error) {
	proxy := cctx.String(ProxyAddr)
	if len(proxy) != 0 {
		return proxy, nil
	}

	proxyfetcher := cctx.String(ProxyFetcherAddr)
	if len(proxyfetcher) == 0 {
		// no proxy and no proxyfetcher
		return "", nil
	}
	return getProxyByFetcher(proxyfetcher, owner)
}

type ProxyLockResponse struct {
	Data Proxy `json:"data,omitempty"`
}

// the following is copied from proxy-lock
type Model struct {
	ID        uint `gorm:"primary_key" json:"id"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time	`json:"updated_at,omitempty"`
	DeletedAt *time.Time `sql:"index" json:"deleted_at,omitempty"`
}

type Proxy struct {
	Model
	Host 		    string	`gorm:"type:varchar(15)" json:"host"`
	Port            int		`gorm:"type:int;not null" json:"port"`
	Protocol        string	`gorm:"type:varchar(5);not null;default:'https'" json:"protocol,omitempty"`
	Owner           string	`gorm:"type:varchar(20);not null;" json:"owner,omitempty"`
	Status          string	`gorm:"type:varchar(10);not null;default:'free'" json:"status,omitempty"`
	DurationMinute  int64   `gorm:"type:integer;not null" json:"DurationMinute"`
	ExpiredAt       time.Time `json:"expired_at,omitempty"`
}

func getProxyByFetcher(fetchAddr string, owner string) (proxy string, err error) {
	var client *http.Client
	client = &http.Client{}

    url := fmt.Sprintf("http://%s/api/v1/proxies/lock", fetchAddr)
	data := fmt.Sprintf("{\"owner\":\"%s\"}", owner)
	var request *http.Request
	request, err = http.NewRequest("POST", url,  strings.NewReader(data))
	if err != nil {
		return "", err
	}

	request.Header.Add("Content-Type", "application/json")

	var response *http.Response
	response, err = client.Do(request)
	if err != nil {
		return "", err
	}
	var body []byte

	var reader io.ReadCloser

	if response.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("proxy lock fail: %d", response.StatusCode)
		log.Warn(msg)
		return "", errors.New(msg)
	}

	reader = response.Body

	body, err = ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}

	log.Infof("response: %s", string(body))

	var r = ProxyLockResponse{}
	err = json.Unmarshal(body, &r)
	if err != nil {
		return "", nil
	}
	if len(r.Data.Host) == 0 {
		return "", errors.New("host field is empty")
	}

	proxy = fmt.Sprintf("%s:%d", r.Data.Host, r.Data.Port)

	return proxy, nil

}

func makeHostKey(lr repo.LockedRepo) (crypto.PrivKey, error) {
	pk, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}

	ks, err := lr.KeyStore()
	if err != nil {
		return nil, err
	}

	kbytes, err := pk.Bytes()
	if err != nil {
		return nil, err
	}

	if err := ks.Put("libp2p-host", types.KeyInfo{
		Type:       "libp2p-host",
		PrivateKey: kbytes,
	}); err != nil {
		return nil, err
	}

	return pk, nil
}
func configureStorageMiner(ctx context.Context, api api.FullNode, addr address.Address, peerid peer.ID) error {
	// This really just needs to be an api call at this point...
	recp, err := api.StateCall(ctx, &types.Message{
		To:     addr,
		From:   addr,
		Method: actors.MAMethods.GetWorkerAddr,
	}, nil)
	if err != nil {
		return xerrors.Errorf("failed to get worker address: %w", err)
	}

	if recp.ExitCode != 0 {
		return xerrors.Errorf("getWorkerAddr returned exit code %d", recp.ExitCode)
	}

	waddr, err := address.NewFromBytes(recp.Return)
	if err != nil {
		return xerrors.Errorf("getWorkerAddr returned bad address: %w", err)
	}

	enc, err := actors.SerializeParams(&actors.UpdatePeerIDParams{PeerID: peerid})
	if err != nil {
		return err
	}

	msg := &types.Message{
		To:       addr,
		From:     waddr,
		Method:   actors.MAMethods.UpdatePeerID,
		Params:   enc,
		Value:    types.NewInt(0),
		GasPrice: types.NewInt(0),
		GasLimit: types.NewInt(100000000),
	}

	smsg, err := api.MpoolPushMessage(ctx, msg)
	if err != nil {
		return err
	}

	log.Info("Waiting for message: ", smsg.Cid())
	ret, err := api.StateWaitMsg(ctx, smsg.Cid())
	if err != nil {
		return err
	}

	if ret.Receipt.ExitCode != 0 {
		return xerrors.Errorf("update peer id message failed with exit code %d", ret.Receipt.ExitCode)
	}

	return nil
}

package cli

import (
	"context"
	"github.com/multiformats/go-multiaddr"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	logging "github.com/ipfs/go-log"
	"github.com/mitchellh/go-homedir"
	manet "github.com/multiformats/go-multiaddr-net"
	"golang.org/x/xerrors"
	"gopkg.in/urfave/cli.v2"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/lib/jsonrpc"
	"github.com/filecoin-project/lotus/node/repo"
)

var log = logging.Logger("cli")

const (
	metadataTraceConetxt = "traceContext"
)

// ApiConnector returns API instance
type ApiConnector func() api.FullNode

func RepoInfo(ctx *cli.Context, repoFlag string) (string, string, error) {
	p, err := homedir.Expand(ctx.String(repoFlag))
	if err != nil {
		return "", "", err
	}

	r, err := repo.NewFS(p)
	if err != nil {
		return "", "", err
	}

	ma, err := r.APIEndpoint()
	if err != nil {
		return "", "", xerrors.Errorf("failed to get api endpoint: (%s) %w", p, err)
	}
	_, addr, err := manet.DialArgs(ma)
	if err != nil {
		return "", "", err
	}

	return p, addr, nil
}

func GetRawAPI(ctx *cli.Context, repoFlag string) (string, http.Header, error) {
	rdir, addr, err := RepoInfo(ctx, repoFlag)
	if err != nil {
		return "", nil, err
	}

	r, err := repo.NewFS(rdir)
	if err != nil {
		return "", nil, err
	}

	var headers http.Header
	token, err := r.APIToken()
	if err != nil {
		log.Warnf("Couldn't load CLI token, capabilities may be limited: %w", err)
	} else {
		headers = http.Header{}
		headers.Add("Authorization", "Bearer "+string(token))
	}

	return "ws://" + addr + "/rpc/v0", headers, nil
}

func getAPIByAddrToken(strma string, token string) (string, http.Header, error) {
	strma = strings.TrimSpace(strma)
	ma, err := multiaddr.NewMultiaddr(strma)
	if err != nil {
		return "", nil, err
	}
	_, addr, err := manet.DialArgs(ma)
	if err != nil {
		return "", nil, err
	}
	var headers http.Header
	headers = http.Header{}
	headers.Add("Authorization", "Bearer "+ token)
	return "ws://" + addr + "/rpc/v0", headers, nil
}

func GetAPI(ctx *cli.Context) (api.Common, jsonrpc.ClientCloser, error) {
	f := "repo"
	if ctx.String("storagerepo") != "" {
		f = "storagerepo"
	}

	addr, headers, err := GetRawAPI(ctx, f)
	if err != nil {
		return nil, nil, err
	}

	return client.NewCommonRPC(addr, headers)
}

func GetFullNodeAPI(ctx *cli.Context) (api.FullNode, jsonrpc.ClientCloser, error) {

	var addr string
	var headers http.Header
	var err error
	fullnode := ctx.String("fullnode-api")
	if len(fullnode) == 0 {
		addr, headers, err = GetRawAPI(ctx, "repo")
	} else {
		token := ctx.String("fullnode-token")
		if len(token) == 0 {
			return nil, nil, xerrors.Errorf("Fullnode token should be provided")
		}
		addr, headers, err = getAPIByAddrToken(fullnode, token)
	}

	if err != nil {
		return nil, nil, err
	}

	return client.NewFullNodeRPC(addr, headers)
}

func GetStorageMinerAPI(ctx *cli.Context) (api.StorageMiner, jsonrpc.ClientCloser, error) {
	addr, headers, err := GetRawAPI(ctx, "storagerepo")
	if err != nil {
		return nil, nil, err
	}

	return client.NewStorageMinerRPC(addr, headers)
}

func DaemonContext(cctx *cli.Context) context.Context {
	if mtCtx, ok := cctx.App.Metadata[metadataTraceConetxt]; ok {
		return mtCtx.(context.Context)
	}

	return context.Background()
}

// ReqContext returns context for cli execution. Calling it for the first time
// installs SIGTERM handler that will close returned context.
// Not safe for concurrent execution.
func ReqContext(cctx *cli.Context) context.Context {
	tCtx := DaemonContext(cctx)

	ctx, done := context.WithCancel(tCtx)
	sigChan := make(chan os.Signal, 2)
	go func() {
		<-sigChan
		done()
	}()
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	return ctx
}

var Commands = []*cli.Command{
	authCmd,
	chainCmd,
	clientCmd,
	createMinerCmd,
	fetchParamCmd,
	mpoolCmd,
	netCmd,
	paychCmd,
	sendCmd,
	stateCmd,
	syncCmd,
	versionCmd,
	walletCmd,
}

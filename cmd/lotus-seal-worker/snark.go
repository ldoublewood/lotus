package main

import (
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	lcli "github.com/filecoin-project/lotus/cli"
)

var snarkCmd = &cli.Command{
	Name:  "snark",
	Usage: "manage snark",
	Subcommands: []*cli.Command{
		snarkAddCmd,
		snarkRemoveCmd,
	},
}

var snarkAddCmd = &cli.Command{
	Name:  "add",
	Usage: "add a snark url",
	Flags: []cli.Flag{
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetWorkerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		if !cctx.Args().Present() {
			return xerrors.Errorf("must specify snark url to add")
		}

		p := cctx.Args().First()
		return nodeApi.AddSnark(ctx, p)
	},
}

var snarkRemoveCmd = &cli.Command{
	Name:  "remove",
	Usage: "remove a snark url",
	Flags: []cli.Flag{
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetWorkerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		if !cctx.Args().Present() {
			return xerrors.Errorf("must specify snark url to remove")
		}

		p := cctx.Args().First()
		return nodeApi.RemoveSnark(ctx, p)
	},
}

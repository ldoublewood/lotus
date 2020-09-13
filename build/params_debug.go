// +build debug

package build


import (
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
)
 
func init() {
	InsecurePoStValidation = true
	BuildType |= BuildDebug
	miner.SupportedProofTypes = map[abi.RegisteredSealProof]struct{}{
		abi.RegisteredSealProof_StackedDrg512MiBV1: {},
	}
}

// NOTE: Also includes settings from params_2k

// +build debug

package build

import (
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/policy"
)

func init() {
	InsecurePoStValidation = true
	BuildType |= BuildDebug
	policy.SetSupportedProofTypes(abi.RegisteredSealProof_StackedDrg512MiBV1)

}

// NOTE: Also includes settings from params_2k

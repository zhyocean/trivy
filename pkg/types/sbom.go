package types

import (
	stypes "github.com/spdx/tools-golang/spdx"

	"github.com/zhyocean/trivy/pkg/fanal/types"
)

type SBOM struct {
	OS           types.OS
	Packages     []types.PackageInfo
	Applications []types.Application

	CycloneDX *types.CycloneDX
	SPDX      *stypes.Document
}

type SBOMSource = string

const (
	SBOMSourceOCI   = SBOMSource("oci")
	SBOMSourceRekor = SBOMSource("rekor")
)

var (
	SBOMSources = []string{
		SBOMSourceOCI,
		SBOMSourceRekor,
	}
)

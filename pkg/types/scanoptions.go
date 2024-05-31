package types

import (
	"github.com/zhyocean/trivy/pkg/fanal/types"
	"github.com/zhyocean/trivy/rpc/common"
)

// ScanOptions holds the attributes for scanning vulnerabilities
type ScanOptions struct {
	OsFamily            string
	OsName              string
	VulnType            []string
	Scanners            Scanners
	ImageConfigScanners Scanners // Scanners for container image configuration
	ScanRemovedPackages bool
	ListAllPackages     bool
	LicenseCategories   map[types.LicenseCategory][]string
	FilePatterns        []string
	Packages            []*common.Package
}

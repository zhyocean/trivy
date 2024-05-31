package mariner

import (
	"strings"

	version "github.com/knqyf263/go-rpm-version"
	"golang.org/x/xerrors"

	"github.com/aquasecurity/trivy-db/pkg/vulnsrc/mariner"
	ftypes "github.com/zhyocean/trivy/pkg/fanal/types"
	"github.com/zhyocean/trivy/pkg/log"
	"github.com/zhyocean/trivy/pkg/scanner/utils"
	"github.com/zhyocean/trivy/pkg/types"
)

// Scanner implements the CBL-Mariner scanner
type Scanner struct {
	vs mariner.VulnSrc
}

// NewScanner is the factory method for Scanner
func NewScanner() *Scanner {
	return &Scanner{
		vs: mariner.NewVulnSrc(),
	}
}

// Detect vulnerabilities in package using CBL-Mariner scanner
func (s *Scanner) Detect(osVer string, _ *ftypes.Repository, pkgs []ftypes.Package) ([]types.DetectedVulnerability, error) {
	log.Logger.Info("Detecting CBL-Mariner vulnerabilities...")

	// e.g. 1.0.20210127
	if strings.Count(osVer, ".") > 1 {
		osVer = osVer[:strings.LastIndex(osVer, ".")]
	}

	log.Logger.Debugf("CBL-Mariner: os version: %s", osVer)
	log.Logger.Debugf("CBL-Mariner: the number of packages: %d", len(pkgs))

	var vulns []types.DetectedVulnerability
	for _, pkg := range pkgs {
		// CBL Mariner OVAL contains source package names only.
		advisories, err := s.vs.Get(osVer, pkg.SrcName)
		if err != nil {
			return nil, xerrors.Errorf("failed to get CBL-Mariner advisories: %w", err)
		}

		sourceVersion := version.NewVersion(utils.FormatSrcVersion(pkg))

		for _, adv := range advisories {
			vuln := types.DetectedVulnerability{
				VulnerabilityID:  adv.VulnerabilityID,
				PkgName:          pkg.Name,
				InstalledVersion: utils.FormatVersion(pkg),
				PkgRef:           pkg.Ref,
				Layer:            pkg.Layer,
				DataSource:       adv.DataSource,
			}

			// Unpatched vulnerabilities
			if adv.FixedVersion == "" {
				vulns = append(vulns, vuln)
				continue
			}

			// Patched vulnerabilities
			fixedVersion := version.NewVersion(adv.FixedVersion)
			if sourceVersion.LessThan(fixedVersion) {
				vuln.FixedVersion = fixedVersion.String()
				vulns = append(vulns, vuln)
			}
		}
	}

	return vulns, nil
}

// IsSupportedVersion checks the OS version can be scanned using CBL-Mariner scanner
func (s *Scanner) IsSupportedVersion(osFamily, osVer string) bool {
	// EOL is not in public at the moment.
	return true
}

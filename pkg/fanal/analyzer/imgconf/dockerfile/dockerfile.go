package dockerfile

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"golang.org/x/xerrors"

	"github.com/zhyocean/trivy/pkg/fanal/analyzer"
	"github.com/zhyocean/trivy/pkg/fanal/image"
	"github.com/zhyocean/trivy/pkg/fanal/types"
	"github.com/zhyocean/trivy/pkg/mapfs"
	"github.com/zhyocean/trivy/pkg/misconf"
)

const analyzerVersion = 1

func init() {
	analyzer.RegisterConfigAnalyzer(analyzer.TypeHistoryDockerfile, newHistoryAnalyzer)
}

type historyAnalyzer struct {
	scanner *misconf.Scanner
}

func newHistoryAnalyzer(opts analyzer.ConfigAnalyzerOptions) (analyzer.ConfigAnalyzer, error) {
	s, err := misconf.NewDockerfileScanner(opts.FilePatterns, opts.MisconfScannerOption)
	if err != nil {
		return nil, xerrors.Errorf("misconfiguration scanner error: %w", err)
	}
	return &historyAnalyzer{
		scanner: s,
	}, nil
}

func (a *historyAnalyzer) Analyze(ctx context.Context, input analyzer.ConfigAnalysisInput) (*analyzer.
	ConfigAnalysisResult, error) {
	if input.Config == nil {
		return nil, nil
	}
	dockerfile := new(bytes.Buffer)
	baseLayerIndex := image.GuessBaseImageIndex(input.Config.History)
	for i := baseLayerIndex + 1; i < len(input.Config.History); i++ {
		h := input.Config.History[i]
		var createdBy string
		switch {
		case strings.HasPrefix(h.CreatedBy, "/bin/sh -c #(nop)"):
			// Instruction other than RUN
			createdBy = strings.TrimPrefix(h.CreatedBy, "/bin/sh -c #(nop)")
		case strings.HasPrefix(h.CreatedBy, "/bin/sh -c"):
			// RUN instruction
			createdBy = strings.ReplaceAll(h.CreatedBy, "/bin/sh -c", "RUN")
		case strings.HasPrefix(h.CreatedBy, "USER"):
			// USER instruction
			createdBy = h.CreatedBy
		case strings.HasPrefix(h.CreatedBy, "HEALTHCHECK"):
			// HEALTHCHECK instruction
			var interval, timeout, startPeriod, retries, command string
			if input.Config.Config.Healthcheck.Interval != 0 {
				interval = fmt.Sprintf("--interval=%s ", input.Config.Config.Healthcheck.Interval)
			}
			if input.Config.Config.Healthcheck.Timeout != 0 {
				timeout = fmt.Sprintf("--timeout=%s ", input.Config.Config.Healthcheck.Timeout)
			}
			if input.Config.Config.Healthcheck.StartPeriod != 0 {
				startPeriod = fmt.Sprintf("--startPeriod=%s ", input.Config.Config.Healthcheck.StartPeriod)
			}
			if input.Config.Config.Healthcheck.Retries != 0 {
				retries = fmt.Sprintf("--retries=%d ", input.Config.Config.Healthcheck.Retries)
			}
			command = strings.Join(input.Config.Config.Healthcheck.Test, " ")
			command = strings.ReplaceAll(command, "CMD-SHELL", "CMD")
			createdBy = fmt.Sprintf("HEALTHCHECK %s%s%s%s%s", interval, timeout, startPeriod, retries, command)
		}
		dockerfile.WriteString(strings.TrimSpace(createdBy) + "\n")
	}

	fsys := mapfs.New()
	if err := fsys.WriteVirtualFile("Dockerfile", dockerfile.Bytes(), 0600); err != nil {
		return nil, xerrors.Errorf("mapfs write error: %w", err)
	}

	misconfs, err := a.scanner.Scan(ctx, fsys)
	if err != nil {
		return nil, xerrors.Errorf("history scan error: %w", err)
	}
	// The result should be a single element as it passes one Dockerfile.
	if len(misconfs) != 1 {
		return nil, nil
	}

	return &analyzer.ConfigAnalysisResult{
		Misconfiguration: &misconfs[0],
	}, nil
}

func (a *historyAnalyzer) Required(_ types.OS) bool {
	return true
}

func (a *historyAnalyzer) Type() analyzer.Type {
	return analyzer.TypeHistoryDockerfile
}

func (a *historyAnalyzer) Version() int {
	return analyzerVersion
}

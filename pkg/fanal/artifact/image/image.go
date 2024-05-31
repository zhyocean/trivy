package image

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/samber/lo"
	"golang.org/x/exp/slices"
	"golang.org/x/xerrors"

	"github.com/zhyocean/trivy/pkg/fanal/analyzer"
	"github.com/zhyocean/trivy/pkg/fanal/artifact"
	"github.com/zhyocean/trivy/pkg/fanal/cache"
	"github.com/zhyocean/trivy/pkg/fanal/handler"
	"github.com/zhyocean/trivy/pkg/fanal/image"
	"github.com/zhyocean/trivy/pkg/fanal/log"
	"github.com/zhyocean/trivy/pkg/fanal/types"
	"github.com/zhyocean/trivy/pkg/fanal/walker"
	"github.com/zhyocean/trivy/pkg/mapfs"
	"github.com/zhyocean/trivy/pkg/parallel"
	"github.com/zhyocean/trivy/pkg/semaphore"
	"github.com/zhyocean/trivy/pkg/syncx"
)

type Artifact struct {
	image          types.Image
	cache          cache.ArtifactCache
	walker         walker.LayerTar
	analyzer       analyzer.AnalyzerGroup       // analyzer for files in container image
	configAnalyzer analyzer.ConfigAnalyzerGroup // analyzer for container image config
	handlerManager handler.Manager

	artifactOption artifact.Option
}

type LayerInfo struct {
	DiffID    string
	CreatedBy string // can be empty
}

func NewArtifact(img types.Image, c cache.ArtifactCache, opt artifact.Option) (artifact.Artifact, error) {
	// Initialize handlers
	handlerManager, err := handler.NewManager(opt)
	if err != nil {
		return nil, xerrors.Errorf("handler init error: %w", err)
	}

	a, err := analyzer.NewAnalyzerGroup(analyzer.AnalyzerOptions{
		Group:                opt.AnalyzerGroup,
		Slow:                 opt.Slow,
		FilePatterns:         opt.FilePatterns,
		DisabledAnalyzers:    opt.DisabledAnalyzers,
		MisconfScannerOption: opt.MisconfScannerOption,
		SecretScannerOption:  opt.SecretScannerOption,
		LicenseScannerOption: opt.LicenseScannerOption,
	})
	if err != nil {
		return nil, xerrors.Errorf("analyzer group error: %w", err)
	}

	ca, err := analyzer.NewConfigAnalyzerGroup(analyzer.ConfigAnalyzerOptions{
		FilePatterns:         opt.FilePatterns,
		DisabledAnalyzers:    opt.DisabledAnalyzers,
		MisconfScannerOption: opt.MisconfScannerOption,
	})
	if err != nil {
		return nil, xerrors.Errorf("config analyzer group error: %w", err)
	}

	return Artifact{
		image:          img,
		cache:          c,
		walker:         walker.NewLayerTar(opt.SkipFiles, opt.SkipDirs, opt.Slow),
		analyzer:       a,
		configAnalyzer: ca,
		handlerManager: handlerManager,

		artifactOption: opt,
	}, nil
}

func (a Artifact) Inspect(ctx context.Context) (types.ArtifactReference, error) {
	imageID, err := a.image.ID()
	if err != nil {
		return types.ArtifactReference{}, xerrors.Errorf("unable to get the image ID: %w", err)
	}

	configFile, err := a.image.ConfigFile()
	if err != nil {
		return types.ArtifactReference{}, xerrors.Errorf("unable to get the image's config file: %w", err)
	}

	diffIDs := a.diffIDs(configFile)

	// Debug
	log.Logger.Debugf("Image ID: %s", imageID)
	log.Logger.Debugf("Diff IDs: %v", diffIDs)

	// Try retrieving a remote SBOM document
	if res, err := a.retrieveRemoteSBOM(ctx); err == nil {
		// Found SBOM
		return res, nil
	} else if !errors.Is(err, errNoSBOMFound) {
		// Fail on unexpected error, otherwise it falls into the usual scanning.
		return types.ArtifactReference{}, xerrors.Errorf("remote SBOM fetching error: %w", err)
	}

	// Try to detect base layers.
	baseDiffIDs := a.guessBaseLayers(diffIDs, configFile)
	log.Logger.Debugf("Base Layers: %v", baseDiffIDs)

	// Convert image ID and layer IDs to cache keys
	imageKey, layerKeys, err := a.calcCacheKeys(imageID, diffIDs)
	if err != nil {
		return types.ArtifactReference{}, err
	}

	// Parse histories and extract a list of "created_by"
	layerKeyMap := a.consolidateCreatedBy(diffIDs, layerKeys, configFile)

	missingImage, missingLayers, err := a.cache.MissingBlobs(imageKey, layerKeys)
	if err != nil {
		return types.ArtifactReference{}, xerrors.Errorf("unable to get missing layers: %w", err)
	}

	missingImageKey := imageKey
	if missingImage {
		log.Logger.Debugf("Missing image ID in cache: %s", imageID)
	} else {
		missingImageKey = ""
	}

	if err = a.inspect(ctx, missingImageKey, missingLayers, baseDiffIDs, layerKeyMap, configFile); err != nil {
		return types.ArtifactReference{}, xerrors.Errorf("analyze error: %w", err)
	}

	return types.ArtifactReference{
		Name:    a.image.Name(),
		Type:    types.ArtifactContainerImage,
		ID:      imageKey,
		BlobIDs: layerKeys,
		ImageMetadata: types.ImageMetadata{
			ID:          imageID,
			DiffIDs:     diffIDs,
			RepoTags:    a.image.RepoTags(),
			RepoDigests: a.image.RepoDigests(),
			ConfigFile:  *configFile,
		},
	}, nil
}

func (Artifact) Clean(_ types.ArtifactReference) error {
	return nil
}

func (a Artifact) calcCacheKeys(imageID string, diffIDs []string) (string, []string, error) {
	// Pass an empty config scanner option so that the cache key can be the same, even when policies are updated.
	imageKey, err := cache.CalcKey(imageID, a.configAnalyzer.AnalyzerVersions(), nil, artifact.Option{})
	if err != nil {
		return "", nil, err
	}

	hookVersions := a.handlerManager.Versions()
	var layerKeys []string
	for _, diffID := range diffIDs {
		blobKey, err := cache.CalcKey(diffID, a.analyzer.AnalyzerVersions(), hookVersions, a.artifactOption)
		if err != nil {
			return "", nil, err
		}
		layerKeys = append(layerKeys, blobKey)
	}
	return imageKey, layerKeys, nil
}

func (a Artifact) consolidateCreatedBy(diffIDs, layerKeys []string, configFile *v1.ConfigFile) map[string]LayerInfo {
	// save createdBy fields in order of layers
	var createdBy []string
	for _, h := range configFile.History {
		// skip histories for empty layers
		if h.EmptyLayer {
			continue
		}
		c := strings.TrimPrefix(h.CreatedBy, "/bin/sh -c ")
		c = strings.TrimPrefix(c, "#(nop) ")
		createdBy = append(createdBy, c)
	}

	// If history detected incorrect - use only diffID
	// TODO: our current logic may not detect empty layers correctly in rare cases.
	validCreatedBy := len(diffIDs) == len(createdBy)

	layerKeyMap := map[string]LayerInfo{}
	for i, diffID := range diffIDs {

		c := ""
		if validCreatedBy {
			c = createdBy[i]
		}

		layerKey := layerKeys[i]
		layerKeyMap[layerKey] = LayerInfo{
			DiffID:    diffID,
			CreatedBy: c,
		}
	}
	return layerKeyMap
}

func (a Artifact) inspect(ctx context.Context, missingImage string, layerKeys, baseDiffIDs []string,
	layerKeyMap map[string]LayerInfo, configFile *v1.ConfigFile) error {

	var osFound types.OS
	workers := lo.Ternary(a.artifactOption.Slow, 1, 5)
	p := parallel.NewPipeline(workers, false, layerKeys, func(ctx context.Context, layerKey string) (any, error) {
		layer := layerKeyMap[layerKey]

		// If it is a base layer, secret scanning should not be performed.
		var disabledAnalyzers []analyzer.Type
		if slices.Contains(baseDiffIDs, layer.DiffID) {
			disabledAnalyzers = append(disabledAnalyzers, analyzer.TypeSecret)
		}

		layerInfo, err := a.inspectLayer(ctx, layer, disabledAnalyzers)
		if err != nil {
			return nil, xerrors.Errorf("failed to analyze layer (%s): %w", layer.DiffID, err)
		}
		if err = a.cache.PutBlob(layerKey, layerInfo); err != nil {
			return nil, xerrors.Errorf("failed to store layer: %s in cache: %w", layerKey, err)
		}
		if lo.IsNotEmpty(layerInfo.OS) {
			osFound = layerInfo.OS
		}
		return nil, nil

	}, nil)

	if err := p.Do(ctx); err != nil {
		return xerrors.Errorf("pipeline error: %w", err)
	}

	if missingImage != "" {
		if err := a.inspectConfig(ctx, missingImage, osFound, configFile); err != nil {
			return xerrors.Errorf("unable to analyze config: %w", err)
		}
	}

	return nil
}

func (a Artifact) inspectLayer(ctx context.Context, layerInfo LayerInfo, disabled []analyzer.Type) (types.BlobInfo, error) {
	log.Logger.Debugf("Missing diff ID in cache: %s", layerInfo.DiffID)

	layerDigest, rc, err := a.uncompressedLayer(layerInfo.DiffID)
	if err != nil {
		return types.BlobInfo{}, xerrors.Errorf("unable to get uncompressed layer %s: %w", layerInfo.DiffID, err)
	}
	defer rc.Close()

	// Prepare variables
	var wg sync.WaitGroup
	opts := analyzer.AnalysisOptions{
		Offline:      a.artifactOption.Offline,
		FileChecksum: a.artifactOption.FileChecksum,
	}
	result := analyzer.NewAnalysisResult()
	limit := semaphore.New(a.artifactOption.Slow)

	// Prepare filesystem for post analysis
	files := new(syncx.Map[analyzer.Type, *mapfs.FS])
	tmpDir, err := os.MkdirTemp("", "layers-*")
	if err != nil {
		return types.BlobInfo{}, xerrors.Errorf("mkdir temp error: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Walk a tar layer
	opqDirs, whFiles, err := a.walker.Walk(rc, func(filePath string, info os.FileInfo, opener analyzer.Opener) error {
		if err = a.analyzer.AnalyzeFile(ctx, &wg, limit, result, "", filePath, info, opener, disabled, opts); err != nil {
			return xerrors.Errorf("failed to analyze %s: %w", filePath, err)
		}

		// Build filesystem for post analysis
		if err = a.buildFS(tmpDir, filePath, info, opener, files); err != nil {
			return xerrors.Errorf("failed to build filesystem: %w", err)
		}

		return nil
	})
	if err != nil {
		return types.BlobInfo{}, xerrors.Errorf("walk error: %w", err)
	}

	// Wait for all the goroutine to finish.
	wg.Wait()

	// Post-analysis
	if err = a.analyzer.PostAnalyze(ctx, files, result, opts); err != nil {
		return types.BlobInfo{}, xerrors.Errorf("post analysis error: %w", err)
	}

	// Sort the analysis result for consistent results
	result.Sort()

	blobInfo := types.BlobInfo{
		SchemaVersion:     types.BlobJSONSchemaVersion,
		Digest:            layerDigest,
		DiffID:            layerInfo.DiffID,
		CreatedBy:         layerInfo.CreatedBy,
		OpaqueDirs:        opqDirs,
		WhiteoutFiles:     whFiles,
		OS:                result.OS,
		Repository:        result.Repository,
		PackageInfos:      result.PackageInfos,
		Applications:      result.Applications,
		Misconfigurations: result.Misconfigurations,
		Secrets:           result.Secrets,
		Licenses:          result.Licenses,
		CustomResources:   result.CustomResources,

		// For Red Hat
		BuildInfo: result.BuildInfo,
	}

	// Call post handlers to modify blob info
	if err = a.handlerManager.PostHandle(ctx, result, &blobInfo); err != nil {
		return types.BlobInfo{}, xerrors.Errorf("post handler error: %w", err)
	}

	return blobInfo, nil
}

// buildFS creates filesystem for post analysis
func (a Artifact) buildFS(tmpDir, filePath string, info os.FileInfo, opener analyzer.Opener,
	files *syncx.Map[analyzer.Type, *mapfs.FS]) error {
	// Get all post-analyzers that want to analyze the file
	atypes := a.analyzer.RequiredPostAnalyzers(filePath, info)
	if len(atypes) == 0 {
		return nil
	}

	// Create a temporary file to which the file in the layer will be copied
	// so that all the files will not be loaded into memory
	f, err := os.CreateTemp(tmpDir, "layer-file-*")
	if err != nil {
		return xerrors.Errorf("create temp error: %w", err)
	}
	defer f.Close()

	// Open a file in the layer
	r, err := opener()
	if err != nil {
		return xerrors.Errorf("file open error: %w", err)
	}
	defer r.Close()

	// Copy file content into the temporary file
	if _, err = io.Copy(f, r); err != nil {
		return xerrors.Errorf("copy error: %w", err)
	}

	if err = os.Chmod(f.Name(), info.Mode()); err != nil {
		return xerrors.Errorf("chmod error: %w", err)
	}

	// Create fs.FS for each post-analyzer that wants to analyze the current file
	for _, at := range atypes {
		fsys, _ := files.LoadOrStore(at, mapfs.New())
		if dir := filepath.Dir(filePath); dir != "." {
			if err := fsys.MkdirAll(dir, os.ModePerm); err != nil && !errors.Is(err, fs.ErrExist) {
				return xerrors.Errorf("mapfs mkdir error: %w", err)
			}
		}
		err = fsys.WriteFile(filePath, f.Name())
		if err != nil {
			return xerrors.Errorf("mapfs write error: %w", err)
		}
	}
	return nil
}

func (a Artifact) diffIDs(configFile *v1.ConfigFile) []string {
	if configFile == nil {
		return nil
	}
	return lo.Map(configFile.RootFS.DiffIDs, func(diffID v1.Hash, _ int) string {
		return diffID.String()
	})
}

func (a Artifact) uncompressedLayer(diffID string) (string, io.ReadCloser, error) {
	// diffID is a hash of the uncompressed layer
	h, err := v1.NewHash(diffID)
	if err != nil {
		return "", nil, xerrors.Errorf("invalid layer ID (%s): %w", diffID, err)
	}

	layer, err := a.image.LayerByDiffID(h)
	if err != nil {
		return "", nil, xerrors.Errorf("failed to get the layer (%s): %w", diffID, err)
	}

	// digest is a hash of the compressed layer
	var digest string
	if a.isCompressed(layer) {
		d, err := layer.Digest()
		if err != nil {
			return "", nil, xerrors.Errorf("failed to get the digest (%s): %w", diffID, err)
		}
		digest = d.String()
	}

	rc, err := layer.Uncompressed()
	if err != nil {
		return "", nil, xerrors.Errorf("failed to get the layer content (%s): %w", diffID, err)
	}
	return digest, rc, nil
}

// ref. https://github.com/google/go-containerregistry/issues/701
func (a Artifact) isCompressed(l v1.Layer) bool {
	_, uncompressed := reflect.TypeOf(l).Elem().FieldByName("UncompressedLayer")
	return !uncompressed
}

func (a Artifact) inspectConfig(ctx context.Context, imageID string, osFound types.OS, config *v1.ConfigFile) error {
	result := lo.FromPtr(a.configAnalyzer.AnalyzeImageConfig(ctx, osFound, config))

	info := types.ArtifactInfo{
		SchemaVersion:    types.ArtifactJSONSchemaVersion,
		Architecture:     config.Architecture,
		Created:          config.Created.Time,
		DockerVersion:    config.DockerVersion,
		OS:               config.OS,
		Misconfiguration: result.Misconfiguration,
		Secret:           result.Secret,
		HistoryPackages:  result.HistoryPackages,
	}

	if err := a.cache.PutArtifact(imageID, info); err != nil {
		return xerrors.Errorf("failed to put image info into the cache: %w", err)
	}

	return nil
}

// guessBaseLayers guesses layers in base image (call base layers).
func (a Artifact) guessBaseLayers(diffIDs []string, configFile *v1.ConfigFile) []string {
	if configFile == nil {
		return nil
	}

	baseImageIndex := image.GuessBaseImageIndex(configFile.History)

	// Diff IDs don't include empty layers, so the index is different from histories
	var diffIDIndex int
	var baseDiffIDs []string
	for i, h := range configFile.History {
		// It is no longer base layer.
		if i > baseImageIndex {
			break
		}
		// Empty layers are not included in diff IDs.
		if h.EmptyLayer {
			continue
		}

		if diffIDIndex >= len(diffIDs) {
			// something wrong...
			return nil
		}
		baseDiffIDs = append(baseDiffIDs, diffIDs[diffIDIndex])
		diffIDIndex++
	}
	return baseDiffIDs
}

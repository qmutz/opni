package patch

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
	controlv1 "github.com/rancher/opni/pkg/apis/control/v1"
	"github.com/rancher/opni/pkg/config/v1beta1"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

type FilesystemCache struct {
	CacheMetricsTracker
	config     v1beta1.FilesystemCacheSpec
	fs         afero.Afero
	logger     *zap.SugaredLogger
	cacheGroup singleflight.Group
	patcher    BinaryPatcher
}

var _ Cache = (*FilesystemCache)(nil)

func NewFilesystemCache(fsys afero.Fs, conf v1beta1.FilesystemCacheSpec, patcher BinaryPatcher, lg *zap.SugaredLogger) (Cache, error) {
	if err := fsys.MkdirAll(conf.Dir, 0777); err != nil {
		return nil, err
	}
	if err := fsys.MkdirAll(filepath.Join(conf.Dir, "plugins"), 0777); err != nil {
		return nil, err
	}
	if err := fsys.MkdirAll(filepath.Join(conf.Dir, "patches"), 0777); err != nil {
		return nil, err
	}
	cache := &FilesystemCache{
		config:  conf,
		fs:      afero.Afero{Fs: fsys},
		patcher: patcher,
		logger:  lg,
		CacheMetricsTracker: NewCacheMetricsTracker(map[string]string{
			"cache_type": "filesystem",
		}),
	}
	cache.recomputeDiskStats()
	return cache, nil
}

func (p *FilesystemCache) Archive(manifest *controlv1.PluginArchive) error {
	var group errgroup.Group
	p.logger.Infof("compressing and archiving plugins...")
	for _, item := range manifest.Items {
		destPath := p.path("plugins", item.Metadata.Digest)
		// check if the plugin already exists
		if _, err := p.fs.Stat(destPath); err == nil {
			src, err := p.fs.Open(destPath)
			if err != nil {
				return err
			}
			// verify the hash of the existing plugin
			b2hash, _ := blake2b.New256(nil)
			srcDecoder, err := zstd.NewReader(src)
			if err != nil {
				return err
			}
			_, err = io.Copy(b2hash, srcDecoder)
			src.Close()
			if err == nil && hex.EncodeToString(b2hash.Sum(nil)) == item.Metadata.Digest {
				// the plugin already exists and its hash matches
				continue
			}

			p.logger.With(
				"plugin", item.Metadata.Filename,
			).Warn("existing cached plugin is corrupted, overwriting")
		}

		item := item
		// copy plugins into the cache
		group.Go(func() error {
			dest, err := p.fs.Create(destPath)
			if err != nil {
				return err
			}
			defer dest.Close()
			destEncoder, err := zstd.NewWriter(dest, zstd.WithEncoderLevel(zstd.SpeedDefault))
			if err != nil {
				return err
			}
			defer destEncoder.Close()
			if bytes, err := io.Copy(destEncoder, bytes.NewReader(item.Data)); err != nil {
				return err
			} else {
				p.AddToTotalSizeBytes(item.Metadata.Digest, bytes)
			}
			p.AddToPluginCount(1)
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		p.logger.With(
			zap.Error(err),
		).Error("failed to archive one or more plugins")
		return err
	}
	p.logger.Debugf("added %d new plugins to cache", len(manifest.Items))
	return nil
}

func (p *FilesystemCache) generatePatch(oldDigest, newDigest string) ([]byte, error) {
	oldBin, err := p.GetPlugin(oldDigest)
	if err != nil {
		return nil, err
	}
	newBin, err := p.GetPlugin(newDigest)
	if err != nil {
		return nil, err
	}
	out := new(bytes.Buffer)
	if err := p.patcher.GeneratePatch(bytes.NewReader(oldBin), bytes.NewReader(newBin), out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (p *FilesystemCache) RequestPatch(oldDigest, newDigest string) ([]byte, error) {
	key := p.PatchKey(oldDigest, newDigest)
	patchPath := p.path("patches", key)
	var isCaller bool
	patchDataValue, err, shared := p.cacheGroup.Do(key, func() (any, error) {
		isCaller = true
		if _, err := p.fs.Stat(patchPath); err != nil {
			p.CacheMiss(oldDigest, newDigest)
			lg := p.logger.With(
				"from", oldDigest,
				"to", newDigest,
			)
			lg.Info("generating patch")
			start := time.Now()
			if patchData, err := p.generatePatch(oldDigest, newDigest); err != nil {
				lg.With(
					zap.Error(err),
				).Error("failed to generate patch")
				p.generatePatch(oldDigest, newDigest)
				return nil, err
			} else {
				lg.With(
					"took", time.Since(start).String(),
					"size", len(patchData),
				).Debug("patch generated")
				if err := p.fs.WriteFile(patchPath, patchData, 0644); err != nil {
					p.logger.With(
						zap.Error(err),
					).Error("failed to write patch to disk")
					return nil, err
				}
				p.IncPatchCalcSecsTotal(oldDigest, newDigest, float64(time.Since(start).Seconds()))
				p.AddToTotalSizeBytes(oldDigest+"-to-"+newDigest, int64(len(patchData)))
				p.AddToPatchCount(1)
				return patchData, nil
			}
		} else {
			p.CacheHit(oldDigest, newDigest)
		}
		return p.fs.ReadFile(patchPath)
	})
	if err != nil {
		return nil, err
	}
	if shared && !isCaller {
		p.CacheHit(oldDigest, newDigest)
	}
	return patchDataValue.([]byte), nil
}

func (*FilesystemCache) PatchKey(oldDigest, newDigest string) string {
	return fmt.Sprintf("%s-to-%s", oldDigest, newDigest)
}

func (p *FilesystemCache) GetPlugin(hash string) ([]byte, error) {
	src, err := p.fs.Open(p.path("plugins", hash))
	if err != nil {
		return nil, err
	}
	b2hash, _ := blake2b.New256(nil)
	srcDecoder, err := zstd.NewReader(src)
	if err != nil {
		return nil, err
	}
	tee := io.TeeReader(srcDecoder, b2hash)

	pluginData, err := io.ReadAll(tee)
	if err != nil {
		return nil, err
	}
	src.Close()

	if hex.EncodeToString(b2hash.Sum(nil)) != hash {
		defer p.Clean(hash)
		p.logger.With(
			"hash", hash,
		).Error("plugin corrupted: hash mismatch")
		return nil, fmt.Errorf("plugin corrupted: hash mismatch")
	}
	return pluginData, nil
}

func (p *FilesystemCache) Clean(hashes ...string) {
	var pluginsRemoved int64
	var patchesRemoved int64
	for _, hash := range hashes {
		// remove the plugin
		if err := p.fs.Remove(p.path("plugins", hash)); err == nil {
			pluginsRemoved++
		}

		patchesToRemove := []string{}

		// remove any patches that reference this plugin
		if items, err := afero.Glob(p.fs, p.path("patches", fmt.Sprintf("*-to-%s", hash))); err == nil {
			patchesToRemove = append(patchesToRemove, items...)
		}

		if items, err := afero.Glob(p.fs, p.path("patches", fmt.Sprintf("%s-to-*", hash))); err == nil {
			patchesToRemove = append(patchesToRemove, items...)
		}

		for _, f := range patchesToRemove {
			if err := p.fs.Remove(f); err == nil {
				patchesRemoved++
			}
		}
	}

	if pluginsRemoved+patchesRemoved > 0 {
		p.AddToPluginCount(-pluginsRemoved)
		p.AddToPatchCount(-patchesRemoved)
		p.logger.Infof("cleaned %d unreachable objects", pluginsRemoved+patchesRemoved)
	}

	p.recomputeDiskStats()
}

func (p *FilesystemCache) ListDigests() ([]string, error) {
	if entries, err := p.fs.ReadDir(p.path("plugins")); err != nil {
		return nil, err
	} else {
		var hashes []string
		for _, e := range entries {
			hashes = append(hashes, e.Name())
		}
		return hashes, nil
	}
}

func (p *FilesystemCache) path(parts ...string) string {
	return filepath.Join(append([]string{p.config.Dir}, parts...)...)
}

func (p *FilesystemCache) recomputeDiskStats() {
	var pluginCount, patchCount int64
	if entries, err := p.fs.ReadDir(p.path("plugins")); err == nil {
		for _, e := range entries {
			p.SetTotalSizeBytes(e.Name(), e.Size())
			pluginCount++
		}
	}
	if entries, err := p.fs.ReadDir(p.path("patches")); err == nil {
		for _, e := range entries {
			p.SetTotalSizeBytes(e.Name(), e.Size())
			patchCount++
		}
	}
	p.SetPluginCount(pluginCount)
	p.SetPatchCount(patchCount)
}

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxAssetBytes caps uploads and imports so a bad URL cannot fill the disk.
const maxAssetBytes = 32 << 20 // 32 MiB

// AssetStore keeps imported binaries content-addressed under data/assets and
// records their metadata so they can be listed and served by sha+ext.
type AssetStore struct {
	dir string
	db  *Store
	// remote is set when davinci is served to the network (serve --remote):
	// the server's own disk and private network are then off limits to asset
	// imports, which otherwise read any local path and fetch any URL.
	remote bool
}

// NewAssetStore returns a store writing into dataDir/assets.
func NewAssetStore(dataDir string, db *Store) *AssetStore {
	return &AssetStore{dir: filepath.Join(dataDir, "assets"), db: db}
}

// SaveBytes stores b and returns its asset record.
func (a *AssetStore) SaveBytes(b []byte, ext string) (*Asset, error) {
	if len(b) == 0 {
		return nil, errors.New("empty file")
	}
	if len(b) > maxAssetBytes {
		return nil, fmt.Errorf("file too large (%s > %s)", humanSize(int64(len(b))), humanSize(maxAssetBytes))
	}
	sha := sha1Hex(b)
	if ext == "" {
		ext = "bin"
	}
	path := filepath.Join(a.dir, sha+"."+ext)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return nil, fmt.Errorf("write asset: %w", err)
		}
	}
	as := &Asset{
		SHA:       sha,
		Ext:       ext,
		MIME:      http.DetectContentType(b),
		Size:      int64(len(b)),
		CreatedAt: time.Now(),
	}
	if _, err := a.db.db.Exec(`INSERT OR IGNORE INTO assets (sha,ext,mime,size,created_at) VALUES (?,?,?,?,?)`,
		as.SHA, as.Ext, as.MIME, as.Size, dbTime(as.CreatedAt)); err != nil {
		return nil, fmt.Errorf("record asset: %w", err)
	}
	return as, nil
}

// Import reads a local file and stores it.
// errRemoteLocalPath refuses a local-path import on a network-facing server.
var errRemoteLocalPath = errors.New("importing a local file path is disabled on this server — upload the file instead")

func (a *AssetStore) Import(path string) (*Asset, error) {
	if a.remote {
		return nil, errRemoteLocalPath
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("empty path")
	}
	path = strings.TrimPrefix(path, "file://")
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if info.Size() > maxAssetBytes {
		return nil, fmt.Errorf("file too large (%s > %s)", humanSize(info.Size()), humanSize(maxAssetBytes))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxAssetBytes {
		return nil, fmt.Errorf("file too large (> %s)", humanSize(maxAssetBytes))
	}
	return a.SaveBytes(b, safeExt(extOf(path)))
}

// Fetch downloads a URL and stores it. Only http/https is accepted.
func (a *AssetStore) Fetch(url string) (*Asset, error) {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, errors.New("only http:// and https:// URLs can be fetched")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	if a.remote {
		client.Transport = publicOnlyTransport()
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	if resp.ContentLength > maxAssetBytes {
		return nil, fmt.Errorf("remote file too large (> %s)", humanSize(maxAssetBytes))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAssetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if len(b) > maxAssetBytes {
		return nil, fmt.Errorf("remote file too large (> %s)", humanSize(maxAssetBytes))
	}
	ext := safeExt(extOf(strings.SplitN(url, "?", 2)[0]))
	if ext == "" {
		ext = "img"
	}
	return a.SaveBytes(b, ext)
}

// Add resolves a reference to an asset and returns the URL the editor should use.
// It accepts an asset URL, a local path, or an http(s) URL.
func (a *AssetStore) Add(ref string) (string, *Asset, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil, errors.New("empty image reference")
	}
	switch {
	case strings.HasPrefix(ref, "/assets/"):
		sha := strings.TrimSuffix(strings.TrimPrefix(ref, "/assets/"), filepath.Ext(ref))
		return ref, &Asset{SHA: sha, Ext: strings.TrimPrefix(filepath.Ext(ref), ".")}, nil
	case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
		as, err := a.Fetch(ref)
		if err != nil {
			return "", nil, err
		}
		return "/assets/" + as.SHA + "." + as.Ext, as, nil
	default:
		as, err := a.Import(ref)
		if err != nil {
			return "", nil, err
		}
		return "/assets/" + as.SHA + "." + as.Ext, as, nil
	}
}

// Path returns the on-disk path for an asset reference like "/assets/<sha>.<ext>".
func (a *AssetStore) Path(url string) (string, error) {
	name := filepath.Base(strings.TrimPrefix(url, "/assets/"))
	if name == "" || name == "." || name == "/" {
		return "", errors.New("empty asset reference")
	}
	path := filepath.Join(a.dir, name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("asset not found: %s", url)
	}
	return path, nil
}

// publicOnlyTransport dials only public addresses, so a URL import on a
// network-facing server cannot reach localhost, the LAN or the cloud
// metadata service (100.100.100.200 on Aliyun). The check runs on the address
// actually dialled, which also covers redirects and DNS tricks.
func publicOnlyTransport() *http.Transport {
	d := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if isPublicIP(ip.IP) {
					return d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				}
			}
			return nil, fmt.Errorf("refusing to fetch from a non-public address (%s)", host)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || cgnat.Contains(ip))
}

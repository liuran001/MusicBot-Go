package native

import (
	"fmt"
	"os"

	widevine "github.com/iyear/gowidevine"
)

// Widevine L3 device loading for Spotify's MP4/AAC (CENC) decryption.
//
// This package deliberately does NOT embed an RSA private key. Decrypting
// Spotify's Widevine-protected audio requires a Widevine L3 device (.wvd), and
// embedding a private key in the repository is a credential-leakage risk and a
// decision the operator should make explicitly. The operator supplies their own
// device file via the spotify plugin's `wvd_path` config option.

// LoadWVDeviceFile loads a gowidevine L3 device from a .wvd file path.
func LoadWVDeviceFile(path string) (*widevine.Device, error) {
	if path == "" {
		return nil, fmt.Errorf("native: no Widevine device configured — set [plugins.spotify] wvd_path to a .wvd file (needed to decrypt Spotify AAC)")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("native: open wvd %q: %w", path, err)
	}
	defer f.Close()
	dev, err := widevine.NewDevice(widevine.FromWVD(f))
	if err != nil {
		return nil, fmt.Errorf("native: load wvd %q: %w", path, err)
	}
	return dev, nil
}

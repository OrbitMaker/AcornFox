package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Network is where this machine pulls images from (three-platform
// architecture; users in mainland China pull from domestic sources). It is decided once
// by "acornfox install" (--china / --global, else detected) and kept in the
// data directory as network.json. Linux keeps the installer's own choice in
// /etc/acornfox/install.env (ACORNFOX_IN_CHINA=true|false).
type Network struct {
	China   bool     `json:"china"`
	Mirrors []string `json:"mirrors,omitempty"` // empty => runner defaults
	Source  string   `json:"source"`            // "flag" | "detected" | "default"

	// DownloadBase is our own download source for the built-in runtime's
	// files (<base>/runtime/<file>), tried before the mirrors and upstream.
	// Empty until it exists; ACORNFOX_DOWNLOAD_BASE overrides it.
	DownloadBase string `json:"download_base,omitempty"`

	// Gateway is the AcornFox download gateway's address: it hands out
	// short-lived download addresses for files we store ourselves and is
	// tried right after DownloadBase. "" means DefaultGateway; "off" turns it
	// off. ACORNFOX_GATEWAY overrides both (an empty value turns it off).
	Gateway string `json:"gateway,omitempty"`
}

// DefaultGateway is the AcornFox download gateway's public address, used when
// network.json names no gateway. Turn it off with "gateway": "off" in
// network.json or an empty ACORNFOX_GATEWAY.
const DefaultGateway = "https://dl.acornfox.com"

// EffectiveGateway is ACORNFOX_GATEWAY when set (an empty value turns the
// gateway off), else n.Gateway ("off" turns it off), else DefaultGateway.
func (n Network) EffectiveGateway() string {
	if v, ok := os.LookupEnv("ACORNFOX_GATEWAY"); ok {
		return strings.TrimSpace(v)
	}
	switch g := strings.TrimSpace(n.Gateway); g {
	case "":
		return DefaultGateway
	case "off":
		return ""
	default:
		return g
	}
}

// EffectiveDownloadBase is ACORNFOX_DOWNLOAD_BASE when set (an empty value
// disables it), else n.DownloadBase.
func (n Network) EffectiveDownloadBase() string {
	if v, ok := os.LookupEnv("ACORNFOX_DOWNLOAD_BASE"); ok {
		return strings.TrimSpace(v)
	}
	return n.DownloadBase
}

// NetworkFile is <dataDir>/network.json.
func NetworkFile(dataDir string) string { return filepath.Join(dataDir, "network.json") }

// LoadNetwork reads network.json; a missing file means China (the default
// for users we serve first).
func LoadNetwork(dataDir string) Network {
	b, err := os.ReadFile(NetworkFile(dataDir))
	if err != nil {
		return Network{China: true, Source: "default"}
	}
	var n Network
	if json.Unmarshal(b, &n) != nil {
		return Network{China: true, Source: "default"}
	}
	return n
}

// SaveNetwork writes network.json (0600).
func SaveNetwork(dataDir string, n Network) error {
	b, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(NetworkFile(dataDir), append(b, '\n'), 0o600)
}

// LinuxInstallChina reads ACORNFOX_IN_CHINA from install.sh's record; ok is
// false when the file or the key is missing.
func LinuxInstallChina(path string) (china, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, found := strings.CutPrefix(strings.TrimSpace(line), "ACORNFOX_IN_CHINA="); found {
			return strings.TrimSpace(v) == "true", true
		}
	}
	return false, false
}

// LinuxInstallCloud returns the cloud vendor install.sh detected from the
// instance metadata ("aliyun", "tencent", "huawei", "volcengine"), or ""
// when unknown or not on one. Callers must treat values they do not know as
// "no vendor": the installer may learn new vendors before this binary does.
func LinuxInstallCloud(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, found := strings.CutPrefix(strings.TrimSpace(line), "ACORNFOX_CLOUD="); found {
			return strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`))
		}
	}
	return ""
}

// DetectChinaURL is the lookup install.sh also uses.
var DetectChinaURL = "http://ip-api.com/json/?fields=countryCode"

// DetectChina asks which country this machine's public address is in. It
// returns an error when the answer is not available (offline, blocked).
func DetectChina(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DetectChinaURL, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var v struct {
		CountryCode string `json:"countryCode"`
	}
	if json.Unmarshal(b, &v) != nil || v.CountryCode == "" {
		return false, errors.New("没取到所在国家")
	}
	return v.CountryCode == "CN", nil
}

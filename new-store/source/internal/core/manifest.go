package core

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Manifest struct {
	AppName        string
	Version        string
	FpkVersion     string
	DisplayName    string
	Platform       string
	Maintainer     string
	MaintainerURL  string
	Distributor    string
	DistributorURL string
	ServicePort    int
	Description    string
	Source         string
	Checksum       string
}

const manifestFieldWidth = 16

func ParseManifest(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest %q: %w", path, err)
	}
	defer f.Close()

	m := &Manifest{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		key, value, ok := parseManifestLine(line)
		if !ok {
			continue
		}

		switch key {
		case "appname":
			m.AppName = value
		case "version":
			m.Version = value
		case "fpk_version":
			m.FpkVersion = value
		case "display_name":
			m.DisplayName = value
		case "platform":
			m.Platform = value
		case "maintainer":
			m.Maintainer = value
		case "maintainer_url":
			m.MaintainerURL = value
		case "distributor":
			m.Distributor = value
		case "distributor_url":
			m.DistributorURL = value
		case "service_port":
			if value == "" {
				continue
			}
			p, parseErr := strconv.Atoi(value)
			if parseErr != nil {
				return nil, fmt.Errorf("parse service_port from %q: %w", path, parseErr)
			}
			m.ServicePort = p
		case "desc":
			m.Description = value
		case "source":
			m.Source = value
		case "checksum":
			m.Checksum = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan manifest %q: %w", path, err)
	}

	return m, nil
}

func ScanInstalled(appsDir string) ([]Manifest, error) {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, fmt.Errorf("read apps dir %q: %w", appsDir, err)
	}

	apps := make([]Manifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(appsDir, entry.Name(), "manifest")
		m, parseErr := ParseManifest(manifestPath)
		if parseErr != nil {
			if errors.Is(parseErr, os.ErrNotExist) {
				continue
			}
			// One unparseable manifest (foreign app, bad service_port, oversized
			// line) must not blind the store to every OTHER installed app: a
			// scan-wide failure used to flip the whole catalog to not-installed,
			// offering 安装 on apps the daemon had registered
			// (conversun/fnos-apps#280 daidai-panel, #281 mihomo).
			log.Printf("manifest: skipping %s: %v", manifestPath, parseErr)
			continue
		}

		// 不过滤 distributor：真实环境里已装 manifest 的 distributor 是各
		// FPK 作者署名（fnos / 源作者名 / 空），从没有 conversun —— 按
		// conversun 过滤会让扫描恒为空，所有已装应用只能走 daemon 兜底
		// 路径（ReconcileInstalled 强制视为最新），版本比较永远不执行，
		// dock「有更新」恒为 0（2026-09-18 测试机 wb2api 1.9.2→1.10.0
		// 不亮角标实锤）。
		apps = append(apps, *m)
	}

	return apps, nil
}

func parseManifestLine(line string) (key, value string, ok bool) {
	if strings.HasPrefix(line, "#") {
		return "", "", false
	}

	if len(line) > manifestFieldWidth {
		left := line[:manifestFieldWidth]
		right := strings.TrimSpace(line[manifestFieldWidth:])
		if strings.HasPrefix(right, "=") {
			return strings.TrimSpace(left), unquote(strings.TrimSpace(strings.TrimPrefix(right, "="))), true
		}
	}

	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return strings.TrimSpace(parts[0]), unquote(strings.TrimSpace(parts[1])), true
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

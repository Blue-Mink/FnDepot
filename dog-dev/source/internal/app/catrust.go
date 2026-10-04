// Package app —— 本地 CA 与 NAS 系统信任的联动。
//
// 背景（P0-2）：「系统代理」开关让所有新 login shell 的 HTTP(S) 流量走本机代理，
// 其中 GitHub 域名在 MITM 启用时会用本地 CA 动态签证书。若本地 CA 没装进 NAS
// 系统信任，ssh / git 等依赖系统信任的客户端会报「unable to get local issuer
// certificate」。本模块提供三方共用的单一状态源：
//
//   - 信任检测（CAInSystemTrust）：按当前 CA 的 DER 指纹在系统 bundle 中比对；
//   - 安装动作（InstallSystemCA）：幂等写入系统证书目录并重建信任库；
//   - 移除动作（RemoveSystemCAFile）：卸载卫生，防系统残留对已删私钥的信任。
//
// 安全取向：检测不确定时一律判「未信任」（fail-safe——代价是白白走隧道，
// 而不是把客户端证书链搞断）。
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ghpp/ghpp/internal/proxy"
)

// 系统信任相关路径与命令。包级变量便于单测指向临时目录与假命令。
var (
	systemCADir     = "/usr/local/share/ca-certificates"
	systemCAFile    = "/usr/local/share/ca-certificates/ghpp-ca.crt"
	systemCABundle  = "/etc/ssl/certs/ca-certificates.crt"
	updateCACertsExe = "update-ca-certificates"
)

// catrustTTL 是信任状态缓存的有效期。系统信任只会经由本应用的安装/移除或
// 人工操作变化，30 秒足够及时反映，同时避免高频路径反复解析系统 bundle。
const catrustTTL = 30 * time.Second

// caInSystemTrustFor 判断给定 CA 是否已在系统信任 bundle 中。
//
// 用 DER 的 SHA-256 指纹逐张比对 bundle 内证书：CA 被重新生成/导入后指纹
// 变化，系统库里即使还有旧 CA 也会被如实判为「未信任」。
// bundle 缺失、不可读或解析失败一律返回 false（fail-safe）。
func caInSystemTrustFor(ca *proxy.CA) bool {
	if ca == nil {
		return false
	}
	data, err := os.ReadFile(systemCABundle)
	if err != nil {
		return false
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return false
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if certDERFingerprint(cert.Raw) == ca.Fingerprint() {
			return true
		}
	}
}

// certDERFingerprint 把证书 DER 的 SHA-256 格式化成与 CA.Fingerprint 相同
// 的大写冒号分隔形式，便于直接比较。
func certDERFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// installSystemCAFor 把给定 CA 装入系统信任，幂等。
//
// 返回 "already"（指纹已在系统库）或 "installed"（本次新装）。
// 写入后强制重建信任库并以指纹复核；复核不过即安装失败。
func installSystemCAFor(ca *proxy.CA) (string, error) {
	if ca == nil {
		return "", fmt.Errorf("本地 CA 不可用")
	}
	if caInSystemTrustFor(ca) {
		return "already", nil
	}

	if err := os.MkdirAll(systemCADir, 0o755); err != nil {
		return "", fmt.Errorf("创建系统证书目录失败: %w（请确认以 root 运行）", err)
	}
	if err := os.WriteFile(systemCAFile, ca.CertPEM(), 0o644); err != nil {
		return "", fmt.Errorf("写入系统证书失败: %w（请确认以 root 运行）", err)
	}

	out, err := runUpdateCACerts(nil)
	if err != nil {
		return "", fmt.Errorf("重建系统信任失败: %v（输出: %s）", err, truncateLog(out, 300))
	}
	if !caInSystemTrustFor(ca) {
		return "", fmt.Errorf("证书已写入 %s 但未进入系统信任库，请检查 %s 是否可用", systemCAFile, updateCACertsExe)
	}
	return "installed", nil
}

// RemoveSystemCAFile 从系统信任移除本地 CA（幂等，供卸载流程直接调用）。
//
// 文件不存在（从未安装或用户已手动删过）直接返回；
// 移除后以 --fresh 重建信任库。全部失败静默忽略：卸载期系统状态正在拆除，
// 清理失败不能阻断卸载流程。
func RemoveSystemCAFile() { removeSystemCAFile() }

func removeSystemCAFile() {
	if _, err := os.Stat(systemCAFile); err != nil {
		return
	}
	_ = os.Remove(systemCAFile)
	_, _ = runUpdateCACerts([]string{"--fresh"})
}

// runUpdateCACerts 执行系统信任库重建命令，带 90 秒超时。
func runUpdateCACerts(args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, updateCACertsExe, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func truncateLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

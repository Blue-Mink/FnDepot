package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type LinuxPackageManager struct {
	Name   string
	Binary string
}

const AptPackageListTTL = 6 * time.Hour

var DetectLinuxPackageManagerLookPathFunc = exec.LookPath

func DetectLinuxPackageManager() (LinuxPackageManager, error) {
	return DetectLinuxPackageManagerWithLookPath(DetectLinuxPackageManagerLookPathFunc)
}

func DetectLinuxPackageManagerWithLookPath(lookPath func(string) (string, error)) (LinuxPackageManager, error) {
	candidates := []LinuxPackageManager{
		{Name: "apk", Binary: "apk"},
		{Name: "apt", Binary: "apt-get"},
		{Name: "dnf", Binary: "dnf"},
		{Name: "yum", Binary: "yum"},
		{Name: "microdnf", Binary: "microdnf"},
		{Name: "zypper", Binary: "zypper"},
	}

	for _, candidate := range candidates {
		if _, err := lookPath(candidate.Binary); err == nil {
			return candidate, nil
		}
	}

	return LinuxPackageManager{}, errors.New("未检测到可用的 Linux 包管理器（支持 apk/apt/dnf/yum/microdnf/zypper）")
}

func ShouldRefreshAptPackageLists() bool {
	return ShouldRefreshAptPackageListsFromDir("/var/lib/apt/lists", time.Now(), AptPackageListTTL)
}

func ShouldRefreshAptPackageListsFromDir(dir string, now time.Time, ttl time.Duration) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}

	var newest time.Time
	hasIndexFile := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "lock" || strings.HasSuffix(name, ".lock") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		hasIndexFile = true
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}

	if !hasIndexFile {
		return true
	}
	return now.Sub(newest) > ttl
}

func LinuxInstallCommandSpec(manager LinuxPackageManager, packageName string, refreshApt bool) (string, []string, error) {
	switch manager.Name {
	case "apk":
		return manager.Binary, []string{"add", "--no-cache", packageName}, nil
	case "apt":
		script := "export DEBIAN_FRONTEND=noninteractive; "
		if refreshApt {
			script += "echo '[APT] 软件包索引过期，正在刷新...'; apt-get update; "
		}
		script += "echo '[APT] 正在安装软件包...'; apt-get install -y --no-install-recommends " + shellQuoteLinuxPackage(packageName)
		return "sh", []string{"-lc", script}, nil
	case "dnf", "yum", "microdnf":
		return manager.Binary, []string{"install", "-y", packageName}, nil
	case "zypper":
		return manager.Binary, []string{"--non-interactive", "install", packageName}, nil
	default:
		return "", nil, errors.New("不支持的 Linux 包管理器")
	}
}

func LinuxRemoveCommandSpec(manager LinuxPackageManager, packageName string, force bool) (string, []string, error) {
	switch manager.Name {
	case "apk":
		args := []string{"del"}
		if force {
			args = append(args, "--force-broken-world")
		}
		args = append(args, packageName)
		return manager.Binary, args, nil
	case "apt":
		args := []string{"remove", "-y"}
		if force {
			args = append(args, "--allow-remove-essential", "--purge")
		}
		args = append(args, packageName)
		return manager.Binary, args, nil
	case "dnf", "yum", "microdnf":
		return manager.Binary, []string{"remove", "-y", packageName}, nil
	case "zypper":
		return manager.Binary, []string{"--non-interactive", "remove", packageName}, nil
	default:
		return "", nil, errors.New("不支持的 Linux 包管理器")
	}
}

// EnsureLinuxPackageManagerPrivilege 在非 root 时提前拦下并说清楚原因。
//
// 背景（v3.0.7）：容器配了 PUID/PGID 之后面板以普通用户跑，而 apt-get / apk 这类
// 系统包管理器必须 root 才能写 /usr、/var/lib/dpkg 以及包管理器自己的锁。
// 原来这条路会一路跑到包管理器自己报一串英文 Permission denied / 锁失败，
// 用户很容易当成面板的 bug。Node.js / Python 依赖不受影响 —— 它们装在数据目录里，
// 那个目录 entrypoint 已经 chown 给运行用户了。
func EnsureLinuxPackageManagerPrivilege() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if os.Geteuid() == 0 {
		return nil
	}
	// 出路要按部署形态给，不能一律写 Docker：仓库自己也推荐二进制部署用
	// packaging/linux/daidai-panel.service 的 User=daidai 非 root 跑，
	// 给那类用户一段 docker exec 的指引等于没给。
	hint := "去掉降权配置改回 root 运行，或以 root 身份手动安装该软件包"
	if _, err := os.Stat("/.dockerenv"); err == nil {
		hint = "在宿主机执行 docker exec -u 0 <容器名> apk add <包名>" +
			"（Debian 版镜像用 apt-get install -y <包名>），" +
			"或去掉 compose 里的 PUID/PGID 让容器回到 root 运行"
	} else if strings.TrimSpace(os.Getenv("DAIDAI_MAGISK_MODULE")) != "" {
		hint = "进容器后以 root 手动安装：参考模块 README 的「进入容器」命令"
	} else {
		hint = "以 root 手动安装该软件包，或去掉 systemd 单元里的 User= / Group= 让面板回到 root 运行"
	}

	return fmt.Errorf("当前面板以非 root 用户（uid=%d）运行，无法安装或卸载 Linux 系统依赖 —— "+
		"apt-get / apk 需要 root 权限才能写 /usr 与包管理器的锁，这是降权运行的固有限制。"+
		"可选做法：%s。"+
		"注意 Node.js / Python 依赖不受此限制，降权下仍可在面板里正常安装", os.Geteuid(), hint)
}

func BuildLinuxPackageCommand(manager LinuxPackageManager, action, packageName string, force bool, distribution string, ensureMirror func(LinuxPackageManager, string) error) (*exec.Cmd, error) {
	// 装和卸都要写系统目录，两条路都得先过这道闸。
	if err := EnsureLinuxPackageManagerPrivilege(); err != nil {
		return nil, err
	}

	switch action {
	case "install":
		refreshApt := manager.Name == "apt" && ShouldRefreshAptPackageLists()
		if ensureMirror != nil {
			if mirrorErr := ensureMirror(manager, distribution); mirrorErr != nil {
				return nil, mirrorErr
			}
		}
		bin, args, err := LinuxInstallCommandSpec(manager, packageName, refreshApt)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(bin, args...)
		cmd.Env = AppendProxyEnv(append(os.Environ(), "TMPDIR=/tmp"))
		return cmd, nil
	case "remove":
		bin, args, err := LinuxRemoveCommandSpec(manager, packageName, force)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(bin, args...)
		cmd.Env = AppendProxyEnv(append(os.Environ(), "TMPDIR=/tmp", "DEBIAN_FRONTEND=noninteractive"))
		return cmd, nil
	default:
		return nil, errors.New("不支持的 Linux 依赖操作")
	}
}

func DetectLinuxDistribution() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ID=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "ID="))
		value = strings.Trim(value, `"'`)
		return strings.ToLower(value)
	}

	return ""
}

func shellQuoteLinuxPackage(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

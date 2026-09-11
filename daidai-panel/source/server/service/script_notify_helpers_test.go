package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/middleware"
	"daidai-panel/testutil"
)

func TestBuildNotifyHelperEnvCreatesManagedHelpers(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	scriptsDir := config.C.Data.ScriptsDir
	workDir := filepath.Join(scriptsDir, "nested")

	env, tokenInfo, err := BuildNotifyHelperEnv(scriptsDir, workDir, config.C.Server.Port, nil, time.Hour)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}

	if env["DAIDAI_NOTIFY_URL"] == "" || env["DAIDAI_NOTIFY_TOKEN"] == "" {
		t.Fatalf("expected notify url/token in env, got %#v", env)
	}
	claims, err := middleware.ParseToken(env["DAIDAI_NOTIFY_TOKEN"])
	if err != nil {
		t.Fatalf("parse helper token: %v", err)
	}
	if tokenInfo == nil || tokenInfo.JTI == "" {
		t.Fatalf("expected script token info with jti, got %#v", tokenInfo)
	}
	if claims.ID != tokenInfo.JTI {
		t.Fatalf("expected returned jti %q to match token claim %q", tokenInfo.JTI, claims.ID)
	}

	paths := []string{
		filepath.Join(scriptsDir, notifyPyFilename),
		filepath.Join(scriptsDir, sendNotifyJSFilename),
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read helper %s: %v", path, err)
		}
		if !strings.Contains(string(content), "DAIDAI_PANEL_MANAGED_NOTIFY_HELPER") {
			t.Fatalf("expected helper marker in %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, notifyPyFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected nested notify.py to stay absent, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, sendNotifyJSFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected nested sendNotify.js to stay absent, got err=%v", err)
	}
	if got := env["DAIDAI_SCRIPTS_DIR"]; got != scriptsDir {
		t.Fatalf("expected DAIDAI_SCRIPTS_DIR=%q, got %q", scriptsDir, got)
	}

	if _, err := os.Stat(filepath.Join(root, "data")); err != nil {
		t.Fatalf("expected test data dir to exist: %v", err)
	}
}

func TestBuildNotifyHelperEnvUsesAbsoluteHelperPaths(t *testing.T) {
	testutil.SetupTestEnv(t)

	scriptsDir := filepath.Join(config.C.Data.ScriptsDir, "nested")
	workDir := filepath.Join(config.C.Data.ScriptsDir, "jobs")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}

	env, _, err := BuildNotifyHelperEnv(scriptsDir, workDir, config.C.Server.Port, nil, time.Hour)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}

	for _, key := range []string{"DAIDAI_SCRIPTS_DIR", "DAIDAI_NOTIFY_PY", "DAIDAI_SEND_NOTIFY_JS"} {
		if !filepath.IsAbs(env[key]) {
			t.Fatalf("expected %s to be absolute, got %q", key, env[key])
		}
	}
}

// 验收 A5：通用入口变量必须存在，而历史的 notify 专用变量一个都不能少 ——
// 内置 notify.py / sendNotify.js 和用户既有脚本都在读它们。
func TestBuildNotifyHelperEnvExposesGenericAPIEntrypoint(t *testing.T) {
	testutil.SetupTestEnv(t)

	scriptsDir := config.C.Data.ScriptsDir
	env, tokenInfo, err := BuildNotifyHelperEnv(scriptsDir, scriptsDir, config.C.Server.Port, nil, time.Hour)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}
	if tokenInfo == nil || tokenInfo.JTI == "" {
		t.Fatalf("expected script token info, got %#v", tokenInfo)
	}

	wantBase := fmt.Sprintf("http://127.0.0.1:%d/api/v1", config.C.Server.Port)
	if got := env["DAIDAI_API_BASE"]; got != wantBase {
		t.Fatalf("expected DAIDAI_API_BASE=%q, got %q", wantBase, got)
	}
	if got := env["DAIDAI_NOTIFY_URL"]; got != wantBase+"/notifications/send" {
		t.Fatalf("expected DAIDAI_NOTIFY_URL to stay unchanged, got %q", got)
	}
	if env["DAIDAI_TOKEN"] == "" {
		t.Fatalf("expected DAIDAI_TOKEN to be populated")
	}
	if env["DAIDAI_TOKEN"] != env["DAIDAI_NOTIFY_TOKEN"] {
		t.Fatalf("expected DAIDAI_TOKEN and DAIDAI_NOTIFY_TOKEN to be the same credential, got %q vs %q",
			env["DAIDAI_TOKEN"], env["DAIDAI_NOTIFY_TOKEN"])
	}

	claims, err := middleware.ParseToken(env["DAIDAI_TOKEN"])
	if err != nil {
		t.Fatalf("parse DAIDAI_TOKEN: %v", err)
	}
	if claims.Role != "operator" {
		t.Fatalf("expected operator role on script token, got %q", claims.Role)
	}
}

// 同样的四个变量必须一路走到任务运行时的 env map，而不只是 helper 内部有。
func TestManagedRuntimeEnvMapCarriesScriptAPIEntrypoint(t *testing.T) {
	testutil.SetupTestEnv(t)

	scriptsDir := config.C.Data.ScriptsDir
	envMap, tokenInfo, err := BuildManagedRuntimeEnvMapWithScriptToken(scriptsDir, scriptsDir, nil, time.Hour, "")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}
	if tokenInfo == nil || tokenInfo.JTI == "" {
		t.Fatalf("expected script token info from runtime env builder, got %#v", tokenInfo)
	}

	for _, key := range []string{"DAIDAI_API_BASE", "DAIDAI_TOKEN", "DAIDAI_NOTIFY_URL", "DAIDAI_NOTIFY_TOKEN"} {
		if envMap[key] == "" {
			t.Fatalf("expected %s in task runtime env, got %q", key, envMap[key])
		}
	}

	claims, err := middleware.ParseToken(envMap["DAIDAI_TOKEN"])
	if err != nil {
		t.Fatalf("parse runtime DAIDAI_TOKEN: %v", err)
	}
	if claims.ID != tokenInfo.JTI {
		t.Fatalf("expected runtime token jti %q to match returned jti %q", claims.ID, tokenInfo.JTI)
	}
}

func TestEnsureManagedHelperFileRewritesManagedJSFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), sendNotifyJSFilename)
	if err := os.WriteFile(path, []byte("// "+managedNotifyHelperToken+"\nmodule.exports = {}\n"), 0o644); err != nil {
		t.Fatalf("seed helper file: %v", err)
	}

	if err := ensureManagedHelperFile(path, managedSendNotifyJSContent+"\n"); err != nil {
		t.Fatalf("rewrite managed helper file: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rewritten helper file: %v", err)
	}
	if string(content) != managedSendNotifyJSContent+"\n" {
		t.Fatalf("expected managed JS helper to be refreshed")
	}
}

func TestManagedHelperContentIncludesUsageDocs(t *testing.T) {
	if !strings.Contains(managedNotifyPyContent, "Usage:") {
		t.Fatalf("expected python helper usage docs")
	}
	if !strings.Contains(managedNotifyPyContent, "def send(title, content, ignore_default_config=False, **kwargs):") {
		t.Fatalf("expected python helper send signature docs")
	}
	if !strings.Contains(managedSendNotifyJSContent, "QingLong-style notify entry point.") {
		t.Fatalf("expected js helper entry point docs")
	}
	if !strings.Contains(managedSendNotifyJSContent, "@param {object} params") {
		t.Fatalf("expected js helper JSDoc params")
	}
}

// issue #111 守卫：托管标记里的 " v1" 绝对不能升成 v2 或别的值。
// ensureManagedHelperFile 是靠「文件正文里有没有这枚 token」来区分
// 「面板自己写的托管文件」和「用户手写的同名文件」的。
// 磁盘上已经存在的 v1 文件里当然不含 "v2" 字串，一旦把 token 改掉，
// 这些老文件全都会被误判成用户自定义而**永久停止更新** —— 比 502 更隐蔽的回归。
func TestManagedNotifyHelperTokenStaysV1(t *testing.T) {
	if !strings.HasSuffix(managedNotifyHelperToken, " v1") {
		t.Fatalf("managedNotifyHelperToken 必须仍以 \" v1\" 结尾，当前为 %q", managedNotifyHelperToken)
	}
	// 两份托管正文都必须带上这枚 token，否则面板自己写出去的文件下次也会被当成用户自定义。
	if !strings.Contains(managedNotifyPyContent, managedNotifyHelperToken) {
		t.Fatalf("expected managed token inside notify.py content")
	}
	if !strings.Contains(managedSendNotifyJSContent, managedNotifyHelperToken) {
		t.Fatalf("expected managed token inside sendNotify.js content")
	}
}

// issue #111：面板开代理后，urllib 会把发往面板自身 127.0.0.1:<端口> 的通知请求
// 也一并交给代理，代理直接回 502。修法是显式 opener + **只对回环豁免**：
// request_notify 支持 url= 覆盖成外网地址，无条件关代理会把那种用法在
// 「只能走代理出网」的环境里直接打断。
func TestManagedNotifyPyDisablesProxyOnlyForLoopback(t *testing.T) {
	if !strings.Contains(managedNotifyPyContent, "import urllib.parse") {
		t.Fatalf("expected `import urllib.parse` in notify.py content")
	}
	if !strings.Contains(managedNotifyPyContent, "urllib.request.build_opener(urllib.request.ProxyHandler({}))") {
		t.Fatalf("expected loopback branch to build an opener with ProxyHandler({})")
	}
	// 必须留着 else 分支的裸 build_opener()：只有 ProxyHandler({}) 一条路就等于无条件关代理。
	if !strings.Contains(managedNotifyPyContent, "        opener = urllib.request.build_opener()") {
		t.Fatalf("expected non-loopback branch to keep the default proxy-aware opener")
	}
	// 三种回环写法都要覆盖到：127.x.x.x / localhost / ::1。
	for _, want := range []string{"\"localhost\", \"::1\"", "host.startswith(\"127.\")"} {
		if !strings.Contains(managedNotifyPyContent, want) {
			t.Fatalf("expected loopback host check to contain %q", want)
		}
	}
	// urlopen 用的是模块级默认 opener，回环豁免根本不会生效，必须换成 opener.open。
	if strings.Contains(managedNotifyPyContent, "urllib.request.urlopen(") {
		t.Fatalf("expected urlopen to be replaced by the explicit opener")
	}
	if !strings.Contains(managedNotifyPyContent, "with opener.open(request, timeout=timeout_seconds) as response:") {
		t.Fatalf("expected the request to go through the explicit opener")
	}
	// 绝不能退回 CIDR 写法：127.0.0.1/32 这类 Python 的代理白名单一律匹配不上（已实测）。
	// 只查真正的代码行 —— 注释里恰恰要写着「别用 CIDR」来警告后来人，
	// 一刀切地匹配整份内容会把那条警告注释本身判成违规。
	for _, line := range strings.Split(managedNotifyPyContent, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "127.0.0.1/") {
			t.Fatalf("CIDR 写法在 Python 侧无效，不要用它做回环白名单，问题行: %s", line)
		}
	}
	// 报错分支是脚本作者排障的唯一线索，改 opener 时不能顺手丢掉。
	if !strings.Contains(managedNotifyPyContent, "except urllib.error.HTTPError as err:") ||
		!strings.Contains(managedNotifyPyContent, "except urllib.error.URLError as err:") {
		t.Fatalf("expected HTTPError / URLError branches to be preserved")
	}
}

// 真跑一次：把托管 notify.py 落到临时目录，在「代理环境变量指向一个死端口」的前提下
// 让它给本机 httptest 假面板发通知。修复前请求会被丢给死代理而失败。
// 顺带把 Go 字面量拼出来的 Python 缩进也验了 —— 缩进写坏 python 直接语法错。
func TestManagedNotifyPyReachesLoopbackPanelWithProxyEnv(t *testing.T) {
	// Windows 上 python3.exe 常常是应用商店的占位程序，LookPath 找得到但跑不了，
	// 所以每个候选都要先用 --version 验一遍能不能真跑；Linux 镜像里则可能只有 python3。
	pythonBin := ""
	for _, candidate := range []string{"python", "python3"} {
		found, lookErr := exec.LookPath(candidate)
		if lookErr != nil {
			continue
		}
		if runErr := exec.Command(found, "--version").Run(); runErr != nil {
			continue
		}
		pythonBin = found
		break
	}
	if pythonBin == "" {
		t.Skip("python not found or not usable")
	}

	// httptest 默认就监听 127.0.0.1，正好是要豁免的回环地址。
	const wantMessage = "panel-ok-9f3a"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"message":%q}`, wantMessage)
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, notifyPyFilename), []byte(managedNotifyPyContent+"\n"), 0o644); err != nil {
		t.Fatalf("write managed notify.py: %v", err)
	}
	// control 分支走原始的 urlopen 写法，用来确认「代理环境变量在本机真的生效」；
	// 不做这个对照，本机若自带 bypass 规则，这条用例会假绿。
	driver := `import os
import sys
import urllib.request

if sys.argv[1] == "control":
    request = urllib.request.Request(
        os.environ["DAIDAI_NOTIFY_URL"],
        data=b"{}",
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        response.read()
    print("CONTROL-REACHED-SERVER")
    sys.exit(0)

import notify

print(notify.request_notify("t", "c").get("message", ""))
`
	if err := os.WriteFile(filepath.Join(dir, "check.py"), []byte(driver), 0o644); err != nil {
		t.Fatalf("write driver script: %v", err)
	}

	// 127.0.0.1:1 上不可能有人监听，所以「请求被交给代理」等价于「立刻失败」。
	// no_proxy 显式置空，避免本机既有的 no_proxy 让对照组失去意义。
	const deadProxy = "http://127.0.0.1:1"
	runEnv := append(os.Environ(),
		"DAIDAI_NOTIFY_URL="+server.URL,
		"DAIDAI_NOTIFY_TOKEN=test-token",
		"DAIDAI_NOTIFY_TIMEOUT=5000",
		"http_proxy="+deadProxy,
		"HTTP_PROXY="+deadProxy,
		"https_proxy="+deadProxy,
		"HTTPS_PROXY="+deadProxy,
		"all_proxy="+deadProxy,
		"ALL_PROXY="+deadProxy,
		"no_proxy=",
		"NO_PROXY=",
		"PYTHONDONTWRITEBYTECODE=1",
	)

	control := exec.Command(pythonBin, "check.py", "control")
	control.Dir = dir
	control.Env = runEnv
	if out, err := control.CombinedOutput(); err == nil && strings.Contains(string(out), "CONTROL-REACHED-SERVER") {
		t.Skipf("本机代理环境变量对回环地址不生效，用例失去对照意义: %s", strings.TrimSpace(string(out)))
	}

	cmd := exec.Command(pythonBin, "check.py", "notify")
	cmd.Dir = dir
	cmd.Env = runEnv
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("managed notify.py 在开代理时没能直连面板: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), wantMessage) {
		t.Fatalf("expected panel response %q in output, got %q", wantMessage, string(out))
	}
}

func TestAppendScriptHelperPathsKeepsExistingEntries(t *testing.T) {
	env := map[string]string{
		"NODE_PATH":    "/tmp/node_modules",
		"PYTHONPATH":   "/tmp/site-packages",
		"NODE_OPTIONS": "--trace-warnings",
	}

	AppendScriptHelperPaths(env, "/tmp/scripts")
	AppendScriptHelperPaths(env, "/tmp/scripts")

	if got := env["NODE_PATH"]; !strings.Contains(got, "/tmp/node_modules") || !strings.Contains(got, "/tmp/scripts") {
		t.Fatalf("unexpected NODE_PATH: %q", got)
	}
	if strings.Count(env["NODE_PATH"], "/tmp/scripts") != 1 {
		t.Fatalf("expected deduplicated NODE_PATH, got %q", env["NODE_PATH"])
	}
	if got := env["PYTHONPATH"]; !strings.Contains(got, "/tmp/site-packages") || !strings.Contains(got, "/tmp/scripts") {
		t.Fatalf("unexpected PYTHONPATH: %q", got)
	}
	if got := env["NODE_OPTIONS"]; !strings.Contains(got, "--trace-warnings") || !strings.Contains(got, "/tmp/scripts/sendNotify.js") {
		t.Fatalf("unexpected NODE_OPTIONS: %q", got)
	}
	if strings.Count(env["NODE_OPTIONS"], "/tmp/scripts/sendNotify.js") != 1 {
		t.Fatalf("expected deduplicated NODE_OPTIONS, got %q", env["NODE_OPTIONS"])
	}
}

func TestCleanupManagedHelperCopiesRemovesOnlyManagedNestedHelpers(t *testing.T) {
	scriptsDir := filepath.Join(t.TempDir(), "scripts")
	workDir := filepath.Join(scriptsDir, "nested")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}

	managedNested := filepath.Join(workDir, sendNotifyJSFilename)
	customNested := filepath.Join(workDir, notifyPyFilename)
	if err := os.WriteFile(managedNested, []byte("// "+managedNotifyHelperToken+"\nmodule.exports={}\n"), 0o644); err != nil {
		t.Fatalf("write managed nested helper: %v", err)
	}
	if err := os.WriteFile(customNested, []byte("# custom helper\n"), 0o644); err != nil {
		t.Fatalf("write custom nested helper: %v", err)
	}

	if err := cleanupManagedHelperCopies(scriptsDir, workDir); err != nil {
		t.Fatalf("cleanup helper copies: %v", err)
	}

	if _, err := os.Stat(managedNested); !os.IsNotExist(err) {
		t.Fatalf("expected managed nested helper to be removed, err=%v", err)
	}
	if _, err := os.Stat(customNested); err != nil {
		t.Fatalf("expected custom nested helper to be preserved, err=%v", err)
	}
}

func TestCleanupManagedHelperCopiesUnderRootRemovesManagedCopiesInNestedDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scripts")
	firstNested := filepath.Join(root, "a")
	secondNested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(secondNested, 0o755); err != nil {
		t.Fatalf("mkdir nested dirs: %v", err)
	}

	rootHelper := filepath.Join(root, sendNotifyJSFilename)
	firstHelper := filepath.Join(firstNested, sendNotifyJSFilename)
	secondHelper := filepath.Join(secondNested, notifyPyFilename)
	for _, path := range []string{rootHelper, firstHelper, secondHelper} {
		if err := os.WriteFile(path, []byte("// "+managedNotifyHelperToken+"\n"), 0o644); err != nil {
			t.Fatalf("write helper %s: %v", path, err)
		}
	}

	if err := CleanupManagedHelperCopiesUnderRoot(root); err != nil {
		t.Fatalf("cleanup under root: %v", err)
	}

	if _, err := os.Stat(rootHelper); err != nil {
		t.Fatalf("expected root helper to stay, err=%v", err)
	}
	if _, err := os.Stat(firstHelper); !os.IsNotExist(err) {
		t.Fatalf("expected first nested helper removed, err=%v", err)
	}
	if _, err := os.Stat(secondHelper); !os.IsNotExist(err) {
		t.Fatalf("expected second nested helper removed, err=%v", err)
	}
}

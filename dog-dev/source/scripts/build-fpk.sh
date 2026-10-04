#!/bin/bash
#
# GitHub++ fpk 构建脚本。
#
# 依赖：
#   - go 1.24+      （交叉编译 linux 二进制）
#   - fnpack        （飞牛官方打包工具，https://developer.fnnas.com/）
#   - node 20+ / npm（前端 web/ui：Vue3 + Vite + Tailwind，构建直出 web/static 供 go:embed）
#
# 用法：
#   ./scripts/build-fpk.sh            # 构建 x86 与 arm64 两个 fpk
#   ./scripts/build-fpk.sh x86        # 仅构建 x86
#   ./scripts/build-fpk.sh arm        # 仅构建 arm64
#
# 产物输出到 dist/ 目录。

set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

VERSION="${FPK_VERSION:-1.1.6}"
DIST="${ROOT}/dist"
mkdir -p "${DIST}"

# ---- 构建溯源守卫 ---------------------------------------------------------
# 拒绝从「有未提交改动」的树构建，保住「装机 == 某个提交的干净构建」不变量
# （1.1.14 曾打包未提交树，装机二进制的 vcs.revision 落后于 HEAD）。
#
# 规则：
#   - 已跟踪文件有改动（M/A/D）→ 一律拒绝（那是载荷本身）；
#   - 未跟踪文件 → 只在其落在构建输入内（cmd/ internal/ web/ fpk/ go.mod go.sum）
#     时拒绝——根目录散落的安装脚本、截图等不进包，放行；
#   - 逃生门：--allow-dirty（确实需要从脏树出包时显式声明）。
ALLOW_DIRTY=0
for a in "$@"; do
    [ "$a" = "--allow-dirty" ] && ALLOW_DIRTY=1
done

check_provenance() {
    if ! command -v git >/dev/null 2>&1 || ! git rev-parse --git-dir >/dev/null 2>&1; then
        return 0
    fi
    local dirty tracked_dirty untracked untracked_input
    dirty=$(git status --porcelain=v1 2>/dev/null || true)
    tracked_dirty=$(printf '%s\n' "${dirty}" | grep -v '^??' || true)
    untracked=$(printf '%s\n' "${dirty}" | grep '^?? ' | sed 's/^?? //' || true)
    untracked_input=$(printf '%s\n' "${untracked}" | grep -E '^(cmd|internal|web|fpk)/|^(go\.mod|go\.sum)$' || true)

    if [ -z "${tracked_dirty}" ] && [ -z "${untracked_input}" ]; then
        echo "==> 源码树干净（提交 $(git rev-parse --short HEAD)），继续构建"
        return 0
    fi
    if [ "${ALLOW_DIRTY}" = "1" ]; then
        echo "==> 警告：源码树存在未提交改动（--allow-dirty，产物溯源以当前树为准）："
        printf '%s\n' "${dirty}" | sed -n '1,20p'
        return 0
    fi
    {
        echo "错误：源码树存在未提交改动，拒绝构建："
        [ -n "${tracked_dirty}" ] && echo "  已跟踪文件改动：" && printf '%s\n' "${tracked_dirty}" | sed 's/^/    /'
        [ -n "${untracked_input}" ] && echo "  构建输入内的未跟踪文件：" && printf '%s\n' "${untracked_input}" | sed 's/^/    /'
        echo "请先 git add + git commit 再构建；确需从脏树出包时追加 --allow-dirty。"
    } >&2
    exit 1
}
check_provenance

# commit 注入：默认取当前 HEAD 短哈希，产物 main.commit 与 vcs.revision 可溯源。
COMMIT="${FPK_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo local)}"

# 前端构建（Vue3 + Vite + Tailwind）：web/ui → web/static（go:embed 进主程序）。
# 必须在 go build 之前完成；前端与架构无关，整个构建只跑一次，不按平台重复。
# 产物直出 web/static（vite.config.js 的 outDir + emptyOutDir），后端静态路由零改动。
if [ -d "${ROOT}/web/ui" ]; then
    echo "==> 构建前端 (vite) ..."
    # 本机 node 环境在 /var/apps/nodejs_v24/target/bin，未进 PATH 时补上。
    if ! command -v node >/dev/null 2>&1; then
        export PATH="/var/apps/nodejs_v24/target/bin:${PATH}"
    fi
    if ! command -v node >/dev/null 2>&1; then
        echo "错误：未找到 node，无法构建前端（web/ui）" >&2
        exit 1
    fi
    # npm ci 按 package-lock.json 精确安装（先清 node_modules），保证可复现。
    (cd "${ROOT}/web/ui" && npm ci --no-audit --no-fund && npm run build)
fi

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=$(date +%Y-%m-%dT%H:%M:%S) -X github.com/ghpp/ghpp/internal/api.Version=${VERSION}"

build_one() {
    local platform="$1"   # x86 | arm
    local goarch="$2"     # amd64 | arm64
    # 注意：workdir 必须用相对路径。
    # go.exe 是原生 Windows 程序，在某些 bash 环境下不会把 /d/... 的
    # POSIX 路径转换成 Windows 路径，绝对路径会让二进制被写到
    # "D:\d\CODE\..." 这种错误位置，fpk 里就没有主程序了。
    # 相对路径（脚本已 cd 到仓库根目录）不依赖路径转换，永远安全。
    local workdir=".build/fpk-${platform}"

    echo "==> 构建 ${platform} (${goarch}) 版本 ${VERSION}"

    # 1. 组装打包目录：fpk/ 是工程模板，app/ 下的二进制按架构放入。
    #
    # 注意：这里不能 rm -rf 清空工作目录 —— 部分环境的 rm 带批量删除保护
    # （按"每个回合的删除总数"计数），删多了会直接中断脚本。
    # 因此只删除真正可能残留的旧文件，其余交给 cp -r 覆盖：
    #   - manifest/manifest.bak：会被 sed 改写过，必须重建
    #   - *.fpk：上次打包的产物，留着会让收集步骤取错文件
    #   - app/ui/config：内容会被改版，必须覆盖而不是合并
    #   - app/ghpp：旧架构的二进制，绝不能混进新包
    # 图标文件名与 fpk/ 模板一致，cp -r 会直接覆盖，无需单独删除。
    mkdir -p "${workdir}/app/ui/images"
    rm -f "${workdir}/manifest" "${workdir}/manifest.bak" "${workdir}"/*.fpk
    rm -f "${workdir}/app/ghpp" "${workdir}/app/ui/config"
    # 1.1.x 随包引擎残留（1.2.0 起不再捆绑，绝不能混进新包）：
    rm -f "${workdir}/app/iStoreEnhance" "${workdir}/app/kspeeder.yml"
    cp -r "${ROOT}/fpk/." "${workdir}/"

    # 包根目录的 ICON.PNG / ICON_256.PNG 由 fpk/ 内的副本提供（见仓库 fpk/ 目录）。

    # 2. 更新 manifest 中的版本号与平台。
    sed -i.bak "s/^version=.*/version=${VERSION}/; s/^platform=.*/platform=${platform}/" "${workdir}/manifest"
    rm -f "${workdir}/manifest.bak"

    # 3. 交叉编译。
    echo "    GOOS=linux GOARCH=${goarch} go build ..."
    GOOS=linux GOARCH=${goarch} CGO_ENABLED=0 go build -trimpath -ldflags "${LDFLAGS}" \
        -o "${workdir}/app/ghpp" ./cmd/ghpp

    # 产物守卫：主程序缺失说明编译或路径出了问题，绝不能继续打包，
    # 否则会产出一个"装得上但永远起不来"的空壳包。
    if [ ! -f "${workdir}/app/ghpp" ]; then
        echo "错误：编译产物 ${workdir}/app/ghpp 不存在，中止打包" >&2
        exit 1
    fi
    echo "    主程序大小：$(du -h "${workdir}/app/ghpp" | cut -f1)"

    # 构建溯源：打印二进制实际记录的 VCS 信息，与 git HEAD 对账。
    go version -m "${workdir}/app/ghpp" 2>/dev/null | grep -E 'vcs.revision|vcs.time|vcs.modified' | sed 's/^/    /' || true

    # 3.5 图标刷新前的准备：1.2.0 起不再捆绑 KSpeeder 引擎二进制
    # （kspeeder 改为独立应用依赖，运行时自动探测其端口）。
    echo "    KSpeeder：依赖独立应用（随包引擎已移除）"

    # 4. 刷新图标（512/256/64 与 favicon）。
    echo "    生成图标..."
    go run scripts/icon.go >/dev/null

    # 5. 打包。
    echo "    fnpack build ..."
    (cd "${workdir}" && fnpack build -d .)

    # 6. 收集产物：按平台与版本命名，避免 x86 / arm 互相覆盖。
    local fpk_file
    fpk_file="$(find "${workdir}" -maxdepth 1 -name '*.fpk' -print -quit)"
    if [ -n "${fpk_file}" ]; then
        mv "${fpk_file}" "${DIST}/dog-dev_${VERSION}_${platform}.fpk"
        echo "    产物：dist/dog-dev_${VERSION}_${platform}.fpk"
    else
        echo "    警告：未找到 fpk 产物" >&2
        return 1
    fi
}

TARGET="${1:-all}"

case "${TARGET}" in
x86)
    build_one x86 amd64
    ;;
arm)
    build_one arm arm64
    ;;
all)
    build_one x86 amd64
    build_one arm arm64
    ;;
*)
    echo "未知目标: ${TARGET}（可选 x86 / arm / all）" >&2
    exit 1
    ;;
esac

echo "==> 构建完成"

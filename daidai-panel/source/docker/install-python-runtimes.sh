#!/bin/sh
# 为 Docker 镜像安装指定小版本的 CPython；面板运行时再按版本创建托管 venv。

set -eu

RUNTIME_FLAVOR=${1:-alpine}
TARGET_ARCH=${2:-}
TARGET_VARIANT=${3:-}
PYTHON_STANDALONE_RELEASE=${4:-20260602}
PYTHON_RUNTIME_310=${5:-3.10.20}
PYTHON_RUNTIME_311=${6:-3.11.15}
PYTHON_RUNTIME_312=${7:-3.12.13}
PYTHON_RUNTIME_MODE=${8:-single}
PYTHON_RUNTIME_VERSION=${9:-3.12}

INSTALL_ROOT=${PYTHON_RUNTIME_ROOT:-/opt/daidai-python}
BASE_URL="https://github.com/astral-sh/python-build-standalone/releases/download/${PYTHON_STANDALONE_RELEASE}"

log() {
  printf '[python-runtime] %s\n' "$*"
}

case "$PYTHON_RUNTIME_MODE" in
  all|single)
    ;;
  *)
    log "invalid PYTHON_RUNTIME_MODE=${PYTHON_RUNTIME_MODE}; expected single or all"
    exit 1
    ;;
esac

case "$PYTHON_RUNTIME_VERSION" in
  3.10|3.11|3.12)
    ;;
  *)
    log "invalid PYTHON_RUNTIME_VERSION=${PYTHON_RUNTIME_VERSION}; expected 3.10, 3.11 or 3.12"
    exit 1
    ;;
esac

fetch() {
  url=$1
  out=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
  else
    log "curl/wget unavailable, skip Python runtime installation"
    return 1
  fi
}

python_platform() {
  flavor=$1
  arch=$2
  variant=$3

  case "${flavor}:${arch}" in
    alpine:amd64)
      printf '%s' 'x86_64-unknown-linux-musl'
      ;;
    alpine:arm64)
      printf '%s' 'aarch64-unknown-linux-musl'
      ;;
    debian:amd64)
      printf '%s' 'x86_64-unknown-linux-gnu'
      ;;
    debian:arm64)
      printf '%s' 'aarch64-unknown-linux-gnu'
      ;;
    debian:arm)
      case "$variant" in
        v7|'')
          printf '%s' 'armv7-unknown-linux-gnueabihf'
          ;;
      esac
      ;;
  esac
}

install_python() {
  minor=$1
  full_version=$2
  platform=$3

  archive="cpython-${full_version}+${PYTHON_STANDALONE_RELEASE}-${platform}-install_only.tar.gz"
  url="${BASE_URL}/${archive}"
  tmp="/tmp/${archive}"
  stage="${INSTALL_ROOT}/${minor}.tmp"
  dest="${INSTALL_ROOT}/${minor}"

  log "install Python ${minor} from ${archive}"
  rm -rf "$stage" "$dest"
  mkdir -p "$stage" "$INSTALL_ROOT"
  fetch "$url" "$tmp"
  tar -xzf "$tmp" -C "$stage"
  mv "$stage/python" "$dest"
  rm -rf "$stage" "$tmp"

  ln -sf "${dest}/bin/python${minor}" "/usr/local/bin/python${minor}"
  ln -sf "${dest}/bin/pip${minor}" "/usr/local/bin/pip${minor}"

  export PATH="${dest}/bin:${PATH}"
  export LD_LIBRARY_PATH="${dest}/lib${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"

  # 下载包名、解释器真实版本、pip 和 venv 必须同时通过，避免发布标签与实际版本不一致的镜像。
  actual_version=$("python${minor}" -c 'import platform; print(platform.python_version())')
  if [ "$actual_version" != "$full_version" ]; then
    log "Python ${minor} version mismatch: expected ${full_version}, got ${actual_version}"
    exit 1
  fi
  "python${minor}" -m pip --version
  smoke_venv="/tmp/daidai-python-${minor}-venv-smoke"
  rm -rf "$smoke_venv"
  "python${minor}" -m venv "$smoke_venv"
  smoke_version=$("${smoke_venv}/bin/python" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')
  if [ "$smoke_version" != "$minor" ]; then
    log "Python ${minor} venv version mismatch: got ${smoke_version}"
    exit 1
  fi
  "${smoke_venv}/bin/python" -m pip --version
  rm -rf "$smoke_venv"
}

should_install_python() {
  minor=$1
  if [ "$PYTHON_RUNTIME_MODE" = "all" ]; then
    return 0
  fi
  [ "$minor" = "$PYTHON_RUNTIME_VERSION" ]
}

PLATFORM=$(python_platform "$RUNTIME_FLAVOR" "$TARGET_ARCH" "$TARGET_VARIANT" || true)

if [ -z "$PLATFORM" ]; then
  # 仅 Alpine 386/armv7 默认 3.12 可使用发行版 Python；其他缺资产组合必须失败。
  use_distro_python=false
  if [ "$RUNTIME_FLAVOR" = "alpine" ] && [ "$PYTHON_RUNTIME_MODE" = "single" ] && [ "$PYTHON_RUNTIME_VERSION" = "3.12" ]; then
    case "${TARGET_ARCH}/${TARGET_VARIANT}" in
      386/|arm/v7) use_distro_python=true ;;
    esac
  fi

  if [ "$use_distro_python" = "true" ]; then
    actual_minor=$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')
    if [ "$actual_minor" != "3.12" ]; then
      log "distro Python version mismatch: expected 3.12, got ${actual_minor}"
      exit 1
    fi
    python3 -m pip --version
    smoke_venv=/tmp/daidai-python-distro-venv-smoke
    rm -rf "$smoke_venv"
    python3 -m venv "$smoke_venv"
    smoke_version=$("${smoke_venv}/bin/python" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')
    if [ "$smoke_version" != "3.12" ]; then
      log "distro Python venv version mismatch: expected 3.12, got ${smoke_version}"
      exit 1
    fi
    "${smoke_venv}/bin/python" -m pip --version
    rm -rf "$smoke_venv"

    # 确认真实小版本后再补标准命令名，供显式 python3.12 任务与 pip 调用使用。
    ln -sf "$(command -v python3)" /usr/local/bin/python3.12
    ln -sf "$(command -v pip3)" /usr/local/bin/pip3.12
    ln -sf "$(command -v pip3)" /usr/local/bin/pip
    log "use verified Alpine distro Python 3.12 for arch=${TARGET_ARCH} variant=${TARGET_VARIANT}"
    exit 0
  fi

  log "no standalone CPython asset for flavor=${RUNTIME_FLAVOR} arch=${TARGET_ARCH} variant=${TARGET_VARIANT}; cannot build mode=${PYTHON_RUNTIME_MODE} version=${PYTHON_RUNTIME_VERSION}"
  exit 1
fi

if should_install_python "3.10"; then
  install_python "3.10" "$PYTHON_RUNTIME_310" "$PLATFORM"
fi
if should_install_python "3.11"; then
  install_python "3.11" "$PYTHON_RUNTIME_311" "$PLATFORM"
fi
if should_install_python "3.12"; then
  install_python "3.12" "$PYTHON_RUNTIME_312" "$PLATFORM"
fi

# 让通用 python3 / pip3 落到当前镜像默认版本；all 镜像仍默认 3.12。
# 这样指定版本镜像里的任务和 venv 创建都会优先使用对应小版本。
default_root="${INSTALL_ROOT}/${PYTHON_RUNTIME_VERSION}"
if [ ! -d "${default_root}/bin" ]; then
  log "default Python runtime ${PYTHON_RUNTIME_VERSION} was not installed"
  exit 1
fi
ln -sf "${default_root}/bin/python${PYTHON_RUNTIME_VERSION}" "/usr/local/bin/python3"
ln -sf "${default_root}/bin/pip${PYTHON_RUNTIME_VERSION}" "/usr/local/bin/pip3"
ln -sf "${default_root}/bin/pip${PYTHON_RUNTIME_VERSION}" "/usr/local/bin/pip"

# 最后从通用命令再验一次默认解释器，防止软链接落到其他系统 Python。
default_minor=$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')
if [ "$default_minor" != "$PYTHON_RUNTIME_VERSION" ]; then
  log "default python3 version mismatch: expected ${PYTHON_RUNTIME_VERSION}, got ${default_minor}"
  exit 1
fi
python3 -m pip --version

log "Python runtimes installed under ${INSTALL_ROOT} (mode=${PYTHON_RUNTIME_MODE}, default=${PYTHON_RUNTIME_VERSION})"

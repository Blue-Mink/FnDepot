#!/bin/bash
# iStoreOS FPK - 后台安装 worker（由 install_callback 秒回后异步拉起）
# 进度写入 /tmp/istoreos-install.state；日志 /tmp/istoreos-install.log
set -e
ENV_FILE="$1"
if [ -n "${ENV_FILE}" ] && [ -f "${ENV_FILE}" ]; then set -a; . "${ENV_FILE}"; set +a; fi
TRIM_TEMP_LOGFILE="${TRIM_TEMP_LOGFILE:-/tmp/istoreos-install.log}"
STATE_FILE="/tmp/istoreos-install.state"
LOCK_FILE="/tmp/istoreos-install.lock"
LOG="${TRIM_TEMP_LOGFILE}"
set_state() { echo "$1" > "${STATE_FILE}.tmp" && mv -f "${STATE_FILE}.tmp" "${STATE_FILE}"; echo "[$(date +%T)] STATE: $1" >>"${LOG}" 2>/dev/null || true; }
CUR="启动"
trap 'set_state "failed: ${CUR}（详见 ${LOG}）"' ERR
trap 'set_state "failed: 进程被信号终止($1,$$)"; rm -f "${LOCK_FILE}"; exit 1' TERM HUP INT QUIT
cleanup() { rm -f "${LOCK_FILE}"; }
trap cleanup EXIT
if [ -f "${LOCK_FILE}" ]; then
    OLD="$(cat "${LOCK_FILE}" 2>/dev/null || true)"
    if [ -n "${OLD}" ] && [ "${OLD}" != "$$" ] && kill -0 "${OLD}" 2>/dev/null; then
        echo "worker 已在运行 (pid ${OLD})，本实例退出"; exit 0
    fi
fi
echo $$ > "${LOCK_FILE}"
set_state "starting"

# ---- 输入参数 ----
ISTO_VERSION="${wizard_isto_version:-25.12.5}"
ISTO_CPU="${wizard_isto_cpu:-2}"
ISTO_MEM="${wizard_isto_mem:-1024}"
ISTO_AUTOSTART="${wizard_isto_autostart:-false}"

# ---- 版本 → 官方不可变文件 + SHA256 ----
case "${ISTO_VERSION}" in
    25.12.5)
        IMG_FILE="istoreos-25.12.5-2026091113-x86-64-squashfs-combined-efi.img.gz"
        IMG_SHA256="a89206e3238cc0421561e4f3d6ffb17296d700bd13d7e77fd93ed1f86afdfc30"
        ;;
    24.10.8)
        IMG_FILE="istoreos-24.10.8-2026073111-x86-64-squashfs-combined-efi.img.gz"
        IMG_SHA256="8825143bc8e4ae45f6f828c7e40e1c9a605ba455ce4c7abf691a4e2b3d2e005e"
        ;;
    22.03.7)
        IMG_FILE="istoreos-22.03.7-2025050912-x86-64-squashfs-combined-efi.img.gz"
        IMG_SHA256="bda277850a2ccb37a67e69974d6b43733057d96dfb5a70914228375879b7d2ba"
        ;;
    *)
        echo "⚠️ 未知版本 ${ISTO_VERSION}（可用: 25.12.5/24.10.8/22.03.7），回退到 25.12.5"
        ISTO_VERSION="25.12.5"
        IMG_FILE="istoreos-25.12.5-2026091113-x86-64-squashfs-combined-efi.img.gz"
        IMG_SHA256="a89206e3238cc0421561e4f3d6ffb17296d700bd13d7e77fd93ed1f86afdfc30"
        ;;
esac
IMG_URL="https://fw.koolcenter.com/iStoreOS/x86_64_efi/${IMG_FILE}"

# ---- 内存校验（512 ~ 物理内存-1536，封顶65536）----
MEM_TOTAL_MB=$(awk '/MemTotal/{print int($2/1024); exit}' /proc/meminfo)
MEM_RESERVE_MB=1536
MEM_MAX_MB=$((MEM_TOTAL_MB - MEM_RESERVE_MB))
[ "${MEM_MAX_MB}" -gt 65536 ] && MEM_MAX_MB=65536
case "${ISTO_MEM}" in ''|*[!0-9]*) ISTO_MEM=0 ;; esac
if [ "${ISTO_MEM}" -lt 512 ] || [ "${ISTO_MEM}" -gt "${MEM_MAX_MB}" ]; then
    echo "错误: 内存大小 ${ISTO_MEM}MB 超出允许范围。本机物理内存 ${MEM_TOTAL_MB}MB，需为飞牛系统保留至少 ${MEM_RESERVE_MB}MB，请输入 512~${MEM_MAX_MB} 之间的整数（MB）" | tee -a "${TRIM_TEMP_LOGFILE}"
    exit 1
fi
echo "内存校验通过: ${ISTO_MEM}MB（本机 ${MEM_TOTAL_MB}MB，上限 ${MEM_MAX_MB}MB）"

# ---- 路径与架构 ----
ARCH=$(uname -m)
VM_NAME="istoreos"
SHARE_PATH=""
for p in $(echo "${TRIM_DATA_SHARE_PATHS}" | tr ':' ' '); do
    if [ -d "${p}" ]; then
        SHARE_PATH="${p}"
        break
    fi
done
[ -z "${SHARE_PATH}" ] && {
    echo "错误: 未找到数据共享路径" | tee -a "${TRIM_TEMP_LOGFILE}"
    exit 1
}

OVS_BRIDGE="$(ovs-vsctl list-br 2>/dev/null | head -n 1)"
if [ -z "${OVS_BRIDGE}" ]; then
    echo "错误: 未检测到 OVS 网桥"
    exit 1
fi

case "${ARCH}" in
    x86_64)
        QEMU_BIN="qemu-system-x86_64"
        UEFI_CODE="/usr/share/OVMF/OVMF_CODE.fd"
        UEFI_VARS="/usr/share/OVMF/OVMF_VARS.fd"
        MACHINE="q35"
        ;;
    *)
        echo "不支持的架构: ${ARCH}"
        exit 1
        ;;
esac

# 预置所需工具链
for tool in qemu-img qemu-system-x86_64 python3; do
    command -v "${tool}" >/dev/null 2>&1 || { echo "错误: 缺少 ${tool}"; exit 1; }
done
[ -c /dev/kvm ] || { echo "错误: /dev/kvm 不可用，无法做网络预置"; exit 1; }
[ -f "${UEFI_CODE}" ] && [ -f "${UEFI_VARS}" ] || { echo "错误: 缺少 OVMF 固件"; exit 1; }

IMG_GZ="${SHARE_PATH}/istoreos.img.gz"
IMG_RAW="${SHARE_PATH}/istoreos.img"
QCOW2_FILE="${SHARE_PATH}/istoreos.qcow2"

# ---- 步骤1: 下载官方镜像（重试3次）----
if [ -s "${QCOW2_FILE}" ]; then
    CUR="复用已有磁盘"; set_state "reuse-disk"
echo ">>> 步骤1: 跳过下载（已存在预置后的 ${QCOW2_FILE}）"
    SKIP_PROV=1
else
    CUR="下载镜像 v${ISTO_VERSION}"; set_state "downloading ${ISTO_VERSION}"
echo ">>> 步骤1: 下载 iStoreOS v${ISTO_VERSION} (约240MB)"
    cd "${SHARE_PATH}"
    ok=0
    for t in 1 2 3; do
        if curl -skL --connect-timeout 30 -C - -o "${IMG_GZ}" "${IMG_URL}"; then ok=1; break; fi
        echo "下载第 ${t} 次失败，重试..."
        sleep 3
    done
    [ "${ok}" = "1" ] || { echo "错误: 镜像下载失败"; exit 1; }

    # ---- 步骤2: SHA256 校验 ----
    CUR="SHA256 校验"; set_state "verifying"
echo ">>> 步骤2: SHA256 校验"
    ACTUAL=$(sha256sum "${IMG_GZ}" | awk '{print $1}')
    if [ "${ACTUAL}" != "${IMG_SHA256}" ]; then
        echo "错误: SHA256 不匹配! 期望 ${IMG_SHA256} 实际 ${ACTUAL}"
        rm -f "${IMG_GZ}"
        exit 1
    fi
    echo "✅ SHA256 校验通过"

    # ---- 步骤3: 解压 + grub 注入串口参数 ----
    CUR="解压+grub注入"; set_state "extracting"
echo ">>> 步骤3: 解压 + grub 串口注入"
    if [ ! -s "${IMG_RAW}" ]; then
        # 官方部分版本的 gz 尾部带杂散字节：gunzip 报 "trailing garbage ignored"，
        # 实测 22.03.7 返回 rc=1、24.10.8 返回 rc=2，但解压内容与 SHA 校验过的
        # gz 完全等价——因此只要解出了内容就容忍任意非零 rc。
        gunzip -c "${IMG_GZ}" > "${IMG_RAW}" || {
            rc=$?
            echo "gunzip rc=${rc}（官方镜像尾部杂散字节告警，解压内容完整，继续）"
            [ -s "${IMG_RAW}" ] || { echo "错误: 解压失败（无输出）"; exit 1; }
        }
    fi
    LOOP=$(losetup -fP --show "${IMG_RAW}")
    MNT=$(mktemp -d)
    mount "${LOOP}p1" "${MNT}"
    GRUB_CFG=$(find "${MNT}" -name grub.cfg | head -1)
    [ -n "${GRUB_CFG}" ] || { umount "${MNT}"; losetup -d "${LOOP}"; echo "错误: 镜像内未找到 grub.cfg"; exit 1; }
    awk 'BEGIN{done=0}
         { if(!done && $0 ~ /linux[ \t]+\/boot\/vmlinuz/ && $0 ~ /rompart/ && $0 !~ /console=/ && $0 !~ /failsafe/ \
                && sub(/noinitrd/, "console=tty1 console=ttyS0,115200n8 noinitrd")) done=1
           print }' "${GRUB_CFG}" > "${GRUB_CFG}.new"
    mv "${GRUB_CFG}.new" "${GRUB_CFG}"
    umount "${MNT}"; losetup -d "${LOOP}"
    rmdir "${MNT}" 2>/dev/null || true
    echo "✅ grub 已注入 console=ttyS0"

    # ---- 步骤4: 转 qcow2 ----
    echo ">>> 步骤4: 转换 qcow2"
    qemu-img convert -f raw -O qcow2 "${IMG_RAW}" "${QCOW2_FILE}"

    # ---- 步骤5: A1 网络预置（qemu 一次性起机, 串口注入 uci）----
    CUR="网络预置(首次起机)"; set_state "provisioning"
echo ">>> 步骤5: 网络预置（LAN→DHCP客户端 / 关闭自身DHCP与RA）"
    APP_DIR="${TRIM_PKGDIR:-${TRIM_APPDEST:-/vol1/@appcenter/com.istoreos.vm}}"
    [ -f "${APP_DIR}/bin/istoreos-provision.py" ] || APP_DIR="/var/apps/com.istoreos.vm/target"
    [ -f "${APP_DIR}/bin/istoreos-provision.py" ] || APP_DIR="/var/apps/com.istoreos.vm"
    PROV_SCRIPT="${APP_DIR}/bin/istoreos-provision.py"
    # payload 部署与回调存在时序差，最多等 60s
    for t in $(seq 1 12); do
        [ -f "${PROV_SCRIPT}" ] && break
        sleep 5
    done
    [ -f "${PROV_SCRIPT}" ] || { echo "错误: 找不到预置脚本 istoreos-provision.py"; exit 1; }
    if ! python3 "${PROV_SCRIPT}" "${QCOW2_FILE}" >>"${TRIM_TEMP_LOGFILE}" 2>&1; then
        echo "错误: 网络预置失败（详见 ${TRIM_TEMP_LOGFILE}）。为免装机后 IP 孤立，安装中止。"
        exit 1
    fi
    echo "✅ 网络预置完成"
    SKIP_PROV=0
fi

# ---- 清理中间文件（保留预置后的 qcow2）----
[ "${ISTO_KEEP_IMG:-0}" = "1" ] || rm -f "${IMG_GZ}" "${IMG_RAW}"

# ===== 注册磁盘到 vol1 存储池 (修复显示 0 MB) =====
POOL_NAME="vol1"
POOL_PATH="/vol1/vm/pool"
if virsh pool-info "${POOL_NAME}" &>/dev/null && [ "${SKIP_PROV}" != "1" ]; then
    echo ">>> 注册磁盘到 ${POOL_NAME} 存储池"
    virsh vol-delete --pool "${POOL_NAME}" istoreos.qcow2 2>/dev/null || true
    DISK_VIRT_GB=$(qemu-img info "${QCOW2_FILE}" --output json | \
        python3 -c "import sys,json; d=json.load(sys.stdin); print(f'{d[\"virtual-size\"]/1024/1024/1024:.0f}')")
    virsh vol-create-as "${POOL_NAME}" istoreos.qcow2 "${DISK_VIRT_GB}G" --format qcow2
    cp "${QCOW2_FILE}" "${POOL_PATH}/istoreos.qcow2"
    chown libvirt-qemu:libvirt-qemu "${POOL_PATH}/istoreos.qcow2"
    virsh pool-refresh "${POOL_NAME}"
    rm -f "${QCOW2_FILE}"
    QCOW2_FILE="${POOL_PATH}/istoreos.qcow2"
    echo "✅ 磁盘路径 -> ${QCOW2_FILE}"
elif [ -f "${POOL_PATH}/istoreos.qcow2" ]; then
    QCOW2_FILE="${POOL_PATH}/istoreos.qcow2"
    echo ">>> 复用存储池中已有磁盘 ${QCOW2_FILE}"
fi

DISK_GB=$(qemu-img info "${QCOW2_FILE}" --output json | \
    python3 -c "import sys,json; d=json.load(sys.stdin); print(f'{d[\"virtual-size\"]/1024/1024/1024:.1f}')")
echo "磁盘实际大小: ${DISK_GB} GB"

# ---- UUID + MAC ----
VM_UUID=$(uuidgen)
MAC_ADDR="52:54:$(printf '%02x:%02x:%02x:%02x' $((RANDOM%256)) $((RANDOM%256)) $((RANDOM%256)) $((RANDOM%256)))"

# ---- XML（含完整 metadata，虚拟机应用可正常显示）----
CUR="创建虚拟机"; set_state "defining-vm"
echo ">>> 生成虚拟机 XML"
XML_FILE="${SHARE_PATH}/istoreos.xml"
cat > "${XML_FILE}" << 'XMLBODY'
<domain type='kvm'>
  <name>VM_NAME_PH</name>
  <title>iStoreOS</title>
  <uuid>UUID_PH</uuid>
  <memory unit='MiB'>MEM_PH</memory>
  <currentMemory unit='MiB'>MEM_PH</currentMemory>
  <vcpu placement='static'>CPU_PH</vcpu>
  <os>
    <type arch='ARCH_PH' machine='MACHINE_PH'>hvm</type>
    <loader readonly='yes' type='pflash'>UEFI_CODE_PH</loader>
    <nvram>NVRAM_PH</nvram>
  </os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough' check='none'/>
  <clock offset='utc'>
    <timer name='rtc' tickpolicy='catchup'/>
    <timer name='pit' tickpolicy='delay'/>
    <timer name='hpet' present='no'/>
  </clock>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <emulator>/usr/bin/QEMU_BIN_PH</emulator>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' cache='writeback' io='threads'/>
      <source file='DISK_PATH_PH'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <interface type='bridge'>
      <mac address='MAC_PH'/>
      <source bridge='OVS_PH'/>
      <virtualport type='openvswitch'/>
      <model type='virtio'/>
    </interface>
    <serial type='pty'><target port='0'/></serial>
    <console type='pty'><target type='serial' port='0'/></console>
    <graphics type='vnc' socket='VNC_SOCKET_PH' autoport='yes' power-control='on'>
      <listen type='socket'/>
    </graphics>
    <video><model type='virtio' heads='1' vram='16384'/></video>
    <memballoon model='virtio'><stats period='10'/></memballoon>
  </devices>
  <metadata>
    <customMeta xmlns="customMeta">
      <title xmlns="title">iStoreOS</title>
      <osType xmlns="osType">linux</osType>
      <osVersion xmlns="osVersion">iStoreOS VERSION_PH</osVersion>
      <diskSize xmlns="diskSize">DISKGB_PH</diskSize>
      <autostart xmlns="autostart">AUTOSTART_PH</autostart>
      <createdTime xmlns="createdTime">CTIME_PH</createdTime>
    </customMeta>
  </metadata>
</domain>
XMLBODY

sed -i "s|VM_NAME_PH|${VM_NAME}|g" "${XML_FILE}"
sed -i "s|UUID_PH|${VM_UUID}|g" "${XML_FILE}"
sed -i "s|MEM_PH|${ISTO_MEM}|g" "${XML_FILE}"
sed -i "s|CPU_PH|${ISTO_CPU}|g" "${XML_FILE}"
sed -i "s|ARCH_PH|${ARCH}|g" "${XML_FILE}"
sed -i "s|MACHINE_PH|${MACHINE}|g" "${XML_FILE}"
sed -i "s|UEFI_CODE_PH|${UEFI_CODE}|g" "${XML_FILE}"
sed -i "s|NVRAM_PH|${SHARE_PATH}/istoreos_VARS.fd|g" "${XML_FILE}"
sed -i "s|QEMU_BIN_PH|${QEMU_BIN}|g" "${XML_FILE}"
sed -i "s|DISK_PATH_PH|${QCOW2_FILE}|g" "${XML_FILE}"
sed -i "s|MAC_PH|${MAC_ADDR}|g" "${XML_FILE}"
sed -i "s|OVS_PH|${OVS_BRIDGE}|g" "${XML_FILE}"
sed -i "s|VERSION_PH|${ISTO_VERSION}|g" "${XML_FILE}"
sed -i "s|DISKGB_PH|${DISK_GB}|g" "${XML_FILE}"
sed -i "s|AUTOSTART_PH|${ISTO_AUTOSTART}|g" "${XML_FILE}"
sed -i "s|CTIME_PH|$(date +%s)|g" "${XML_FILE}"
sed -i "s|VNC_SOCKET_PH|/var/run/vms/${VM_NAME}.vnc.sock|g" "${XML_FILE}"

mkdir -p /var/run/vms
chmod 777 /var/run/vms 2>/dev/null || true

echo ">>> UEFI 固件"
[ ! -f "${SHARE_PATH}/istoreos_VARS.fd" ] && cp "${UEFI_VARS}" "${SHARE_PATH}/istoreos_VARS.fd"
chown -R libvirt-qemu:kvm "${QCOW2_FILE}" "${SHARE_PATH}/istoreos_VARS.fd" "${SHARE_PATH}/istoreos.xml" 2>/dev/null || true
chmod 644 "${QCOW2_FILE}" "${SHARE_PATH}/istoreos_VARS.fd" "${SHARE_PATH}/istoreos.xml" 2>/dev/null || true

echo ">>> virsh define"
virsh destroy "${VM_NAME}" 2>/dev/null || true
virsh undefine "${VM_NAME}" --nvram 2>/dev/null || true
virsh define "${XML_FILE}"

if [ "${ISTO_AUTOSTART}" = "true" ]; then
    virsh autostart "${VM_NAME}"
else
    virsh autostart --disable "${VM_NAME}" 2>/dev/null || true
fi

# ===== IP 寻踪转发器（应用中心/桌面图标的固定入口） =====
APP_NAME="com.istoreos.vm"
WEB_PORT=36125
# APP_DIR 必须独立解析：快速路径(复用已预置磁盘)不经过 provision 段，那里赋值不可依赖
APP_DIR="${TRIM_PKGDIR:-${TRIM_APPDEST:-/vol1/@appcenter/com.istoreos.vm}}"
[ -f "${APP_DIR}/bin/istoreos-web-redirect.py" ] || APP_DIR="/var/apps/com.istoreos.vm/target"
[ -f "${APP_DIR}/bin/istoreos-web-redirect.py" ] || APP_DIR="/var/apps/com.istoreos.vm"
REDIRECT_SCRIPT="${APP_DIR}/bin/istoreos-web-redirect.py"
UNIT_FILE="/etc/systemd/system/istoreos-web.service"

if [ -f "${REDIRECT_SCRIPT}" ]; then
    cat > "${UNIT_FILE}" <<EOF
[Unit]
Description=iStoreOS Web Finder (port ${WEB_PORT})
After=network-online.target libvirtd.service trim_app_center.service
Wants=network-online.target

[Service]
Environment=ISTO_VM_NAME=${VM_NAME}
Environment=ISTO_WEB_PORT=${WEB_PORT}
ExecStart=/usr/bin/python3 ${REDIRECT_SCRIPT}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable istoreos-web >/dev/null 2>&1 || true
    ok=0
    for t in 1 2 3; do
        systemctl restart istoreos-web 2>/dev/null || systemctl start istoreos-web 2>/dev/null || true
        sleep 2
        if curl -sf -m 3 "http://127.0.0.1:${WEB_PORT}/healthz" >/dev/null 2>&1; then ok=1; break; fi
    done
    [ "$ok" = "1" ] && echo "✅ IP 寻踪已启动并通过健康检查: http://<NAS_IP>:${WEB_PORT}/" \
                    || echo "⚠️ IP 寻踪健康检查未通过，请在应用中心重启本应用"
else
    echo "⚠️ 未找到 ${REDIRECT_SCRIPT}，跳过 IP 寻踪（应用中心按钮将不可用）"
fi

# ===== 回写 AppCenter（平台会终写数据行，转后台守护持续校验） =====
if command -v psql >/dev/null 2>&1; then
    psql -h /var/run/postgresql -d appcenter -U postgres -tAc \
        "UPDATE app SET service_url='http://' || chr(36) || '{host}:${WEB_PORT}/', status='running', is_stop=true, is_uninstall=true, updated_at=now() WHERE app_name='${APP_NAME}'" \
        >/dev/null 2>&1 || true
    SYNC_SCRIPT="${APP_DIR}/bin/istoreos-db-sync.sh"
    if [ -f "${SYNC_SCRIPT}" ]; then
        pkill -f istoreos-db-sync.sh 2>/dev/null || true
        ISTO_WEB_PORT="${WEB_PORT}" nohup /bin/sh "${SYNC_SCRIPT}" \
            >/tmp/istoreos-db-sync.log 2>&1 </dev/null &
        echo "✅ AppCenter 回写已转入后台守护（首次约需 1-2 分钟生效）"
    else
        echo "⚠️ 缺少 ${SYNC_SCRIPT}，安装后请在应用中心点一次「设置→保存」"
    fi
fi

set_state "ready"
echo "========================================"
echo "✅ iStoreOS 安装完成！"
echo "版本: ${ISTO_VERSION} (EFI/UEFI) | 内存: ${ISTO_MEM}MB | CPU: ${ISTO_CPU}"
echo "网络: 已预置为 DHCP 客户端，接入现有局域网，自动跟随主路由网段"
echo "入口: 应用中心「打开」或桌面 iStoreOS 图标（IP 寻踪自动定位）"
echo "下一步: 在「虚拟机」应用中启动 istoreos 虚拟机"
echo "========================================"

#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""iStoreOS IP 寻踪转发器

监听 NAS 固定端口（默认 36125），为应用中心/桌面图标提供固定入口：
  · GET /...        → 302 到 http://<VM_IP>:80/...（iStoreOS 管理界面）
  · GET /power/start→ 302 回本站 "/"（开机接口只认 POST，见 do_GET 注释）
  · POST /power/start→ 虚拟机关机页的一键开机，回页每 5 秒刷新回 "/"
  · GET /api/status → JSON 状态

跳转前提：解析到的地址必须 TCP:80 探活通过，否则留在寻踪页继续轮询，
避免 ARP 残留/DHCP 旧租约把浏览器甩到一个不通的地址。

虚拟机 IP 多级发现链（找到即缓存 20s，缓存命中先做 TCP 快速验证）：
  1. libvirt XML 的 MAC → /proc/net/arp 反查（最准）
  2. mDNS：avahi-resolve istoreos.local / openwrt.local，TCP:80 验证
  3. virsh domifaddr --source arp/lease/agent
  4. 对本机 LAN 子网做并行 ping 扫描逼出 ARP，再按 MAC 反查
  5. HTTP 指纹补齐：ARP 表活跃主机的 :80 抓取首页，含
     iStoreOS/iStore/Luci 特征即认定为目标（排除 NAS 自身与其他设备）

发现链全空（现实中真实存在：链路通、IPv6 SLAAC 都正常，但 LAN 上没有
任何 DHCPv4 服务器应答，虚拟机就是拿不到 IPv4）时，入口页不再只是干等，
提供三件事，全部经 libvirt 串口在虚拟机里执行，不依赖网络：
  · POST /net/dhcp      → ifup lan + 请 netifd 自己的租约客户端续租（绝不在
                          netifd 托管的口上手跑 udhcpc，见 guest_dhcp_retry 注释）
  · POST /net/restart   → 重启虚拟机网络（治地址在、路由没了的状态错乱）
  · POST /net/static    → 给 LAN 口设静态 IPv4（含恢复自动获取）
  · POST /net/manual    → 只记一个手动跳转地址（虚拟机在别的网段时用）
"""
import http.client
import http.server
import ipaddress
import json
import os
import re
import shlex
import shutil
import socket
import subprocess
import sys
import threading
import time
import urllib.parse
from concurrent.futures import ThreadPoolExecutor

VM_NAME = os.environ.get("ISTO_VM_NAME", "istoreos")
PORT = int(os.environ.get("ISTO_WEB_PORT", "36125"))
TARGET_PORT = int(os.environ.get("ISTO_TARGET_PORT", "80"))
MDNS_NAMES = os.environ.get("ISTO_MDNS_NAMES", "istoreos.local openwrt.local").split()
CACHE_TTL = 20
SWEEP_COOLDOWN = 45
# 刚点过开机的这段时间里，地址不应答多半是系统还在起，不判成死地址
BOOT_GRACE = 90
# 与 cmd/main 共用的两个状态文件：开机请求登记 / 用户主动停用标记
START_REQ = "/tmp/istoreos-vm-start.req"
STOP_MARK = "/tmp/istoreos-user-stopped"
# 应用中心里本应用的身份：入口页叫醒虚拟机后要拿它把平台状态一起带起来，
# 否则平台一直挂着「已停用」，界面上的停用会变成空操作（见 platform_sync_start）
APP_ID = os.environ.get("ISTO_APP_ID", "com.istoreos.vm")
APPCENTER_CLI = "/usr/local/bin/appcenter-cli"
PLAT_SYNC_COOLDOWN = 60     # 秒：冷却期内不重复敲平台
_plat_lock = threading.Lock()
_plat_sync = {"ts": 0.0}
FP_MARKERS = (b"istoreos", b"istore", b"luci")
# 入口页手动记下的跳转地址（放在应用共享目录，重装不丢）：
# 寻踪五层全空时的兜底，比如虚拟机被挪到别的网段。
MANUAL_IP_FILE = "/vol1/@appshare/istoreos/manual-ip"
GUEST_LAN_DEV = "br-lan"

# 必须是**可重入**锁：_resolve_locked() 内部自己也要拿它，而调用方
# （discovery_loop 与 resolve_ip 的冷启动路径）是先持锁再调它。用普通 Lock 时
# 第一次发现就自我死锁——36125 端口只听不答，整个入口页从此卡死
# （1.1.7 引入、1.1.8 修复；离线用例「发现锁必须可重入」守着）。
_lock = threading.RLock()
_cache = {"ip": None, "mac": None, "ts": 0.0, "sweep_ts": 0.0, "source": None}


def _sh(cmd, timeout=10):
    try:
        return subprocess.run(
            cmd, shell=True, capture_output=True, text=True, timeout=timeout
        ).stdout
    except Exception:
        return ""


def tcp_open(ip, port, timeout=0.5):
    try:
        s = socket.create_connection((ip, port), timeout=timeout)
        s.close()
        return True
    except OSError:
        return False


def host_alive(ip, timeout=1):
    """ICMP 探一句：这个地址上到底还有没有机器在应答。

    TCP 端口没通有两种完全不同的情况：系统已经起来、Web 服务还在起（该说
    「Web 服务还在起来」），和 ARP/DHCP 租约里残留的地址压根没人在用（该说
    「还在获取 IP」）。只看端口分不出来，ping 一下就分开了。
    参数不过 valid_v4 不进命令，且用列表传参、不走 shell。
    """
    if not ip or not valid_v4(ip):
        return False
    try:
        return subprocess.run(
            ["ping", "-c", "1", "-W", str(int(timeout)), ip],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            timeout=timeout + 2).returncode == 0
    except Exception:
        return False


def just_powered_on(window=BOOT_GRACE):
    """刚点过「启动虚拟机」或自动补开机不久的宽限期。

    这段时间里地址不应答是正常的（系统还在起），不能马上判成死地址，
    否则每次重启都会先闪一句「还没拿到 IP」。判据是开机请求文件的新鲜度——
    入口按钮、自动补开机、cmd/main 开机都会写它。
    """
    try:
        return time.time() - os.path.getmtime(START_REQ) < window
    except OSError:
        return False


def vm_running():
    return "running" in _sh(f"virsh -c qemu:///system domstate {VM_NAME}", 8)


# 开关机态的 1 秒记忆：virsh 一问要 0.1 秒上下，入口页连打时别每次都敲
_power = {"v": None, "ts": 0.0}
POWER_MEMO = 1.0


def vm_running_now(ttl=POWER_MEMO):
    """带短期记忆的「虚拟机现在到底开没开」。

    发现结论是后台线程维护的（没人看时 20 秒才一轮），拿它当开关机态就会
    出现：虚拟机都关掉了，入口还挂着「正在启动」最长 20 秒，「启动虚拟机」
    按钮迟迟不出来（1.1.10 修）。开关机态本身很便宜，值得实时确认一次。
    """
    now = time.time()
    v = _power["v"]
    if v is not None and now - _power["ts"] < ttl:
        return v
    v = vm_running()
    _power.update(v=v, ts=now)
    return v


def platform_sync_start():
    """把应用中心的状态同步成「已启动」。

    入口页的一键开机/自动补开是直接 virsh start，平台完全不知情：应用中心还挂着
    「已停用」。而它一旦以为自己已经停用，界面上的「停用」就不会再回调 cmd/main
    ——实测 2026-09-14：虚拟机明明在跑，`appcenter-cli stop` 却成了空操作，必须
    先「启用」再「停用」才关得掉，用户只会被莫名卡住。这里补一次
    `appcenter-cli start` 把记账扳回来（实测约 2 秒，且不会重启本服务，pid 不变）。
    """
    now = time.time()
    with _plat_lock:
        if now - _plat_sync["ts"] < PLAT_SYNC_COOLDOWN:
            return
        _plat_sync["ts"] = now
    if os.path.exists(STOP_MARK):
        return          # 用户刚刚点过停用，别跟他抢方向盘
    cli = shutil.which("appcenter-cli") or APPCENTER_CLI
    try:
        subprocess.run([cli, "start", APP_ID], capture_output=True, timeout=10)
    except Exception:
        pass            # 同步失败不影响入口本身，下次开机再试


def platform_sync_start_async():
    """平台记账的活不该拖住 HTTP 响应，丢后台干。"""
    threading.Thread(target=platform_sync_start, daemon=True).start()


def vm_defined():
    return bool(_sh(f"virsh -c qemu:///system dominfo {VM_NAME}", 8).strip())


def vm_power_on():
    """一键/自动开机：shut off→start，paused→resume，running→幂等。返回状态描述。"""
    # 登记开机请求：cmd/main 的关机守望进程据此放弃强制断电（并在关机落定后补开）；
    # 同时撤销「用户停用」标记，否则应用中心会把这个窗口误判成未运行/异常。
    try:
        with open(START_REQ, "w"):
            pass
    except OSError:
        pass
    try:
        os.remove(STOP_MARK)
    except OSError:
        pass
    st = _sh(f"virsh -c qemu:///system domstate {VM_NAME}", 8)
    if "running" in st:
        # 虚拟机本来就在跑也要扳一次记账：可能是上一版留下的背离，也可能是别人
        # 在「虚拟机」应用里开的——平台不知道，停用就会变成空操作。
        platform_sync_start_async()
        return "already-running"
    if not vm_defined():
        return "undefined"
    if "paused" in st:
        _sh(f"virsh -c qemu:///system resume {VM_NAME}", 15)
        platform_sync_start_async()
        return "resumed"
    _sh(f"virsh -c qemu:///system start {VM_NAME}", 20)
    platform_sync_start_async()
    return "starting"


def vm_mac():
    xml = _sh(f"virsh -c qemu:///system dumpxml {VM_NAME}", 8)
    m = re.search(r"<mac address='([0-9a-f:]{17})'", xml)
    if m:
        return m.group(1)
    m = re.search(r"52:54:(?:[0-9a-f]{2}:){3}[0-9a-f]{2}", xml)
    return m.group(0) if m else None


def arp_line_state_ok(flags_field):
    """ARP 表第 3 列 Flags：0x2 才是「已完成」的表项。

    没解析成功的残留项（Flags 0x0）也留在表里，MAC 还写着虚拟机的——
    虚拟机换了地址或根本没开机时，照单全收就会拿着一个死地址告诉用户
    「已找到 192.168.3.x」，一等就是几十分钟。
    """
    try:
        return bool(int(flags_field, 16) & 0x2)
    except (ValueError, TypeError):
        return False


def arp_lookup(mac):
    if not mac:
        return None
    try:
        with open("/proc/net/arp") as f:
            for line in f:
                parts = line.split()
                if (len(parts) >= 4 and parts[3].lower() == mac.lower()
                        and arp_line_state_ok(parts[2])):
                    return parts[0]
    except OSError:
        pass
    return None


def arp_entries():
    ips = []
    try:
        with open("/proc/net/arp") as f:
            for line in f:
                parts = line.split()
                if len(parts) >= 4 and parts[0] != "IP":
                    ips.append(parts[0])
    except OSError:
        pass
    return ips


def local_ips():
    ips = set()
    for line in _sh("ip -4 -o addr show", 8).splitlines():
        m = re.search(r"inet (\d+\.\d+\.\d+\.\d+)", line)
        if m:
            ips.add(m.group(1))
    return ips


def domifaddr():
    for src in ("arp", "lease", "agent"):
        out = _sh(f"virsh -c qemu:///system domifaddr {VM_NAME} --source {src}", 8)
        m = re.search(r"(\d+\.\d+\.\d+\.\d+)/\d+", out)
        if m:
            return m.group(1)
    return None


def arp_mac_of(ip):
    """ARP 表中该 IP 对应的 MAC（无记录返回 None）。"""
    try:
        with open("/proc/net/arp") as f:
            for line in f:
                parts = line.split()
                if (len(parts) >= 4 and parts[0] == ip
                        and arp_line_state_ok(parts[2])):
                    return parts[3].lower()
    except OSError:
        pass
    return None


def mac_ok_for(ip):
    """ARP 守门：若 ARP 表已明确记录该 IP 属于别的 MAC，则否决该候选
    （防止 mDNS/HTTP 指纹误中局域网内其它 OpenWrt/iStore 设备）。"""
    vm = vm_mac()
    if not vm:
        return True
    m = arp_mac_of(ip)
    return m is None or m == vm.lower()


def http_fingerprint(ip, port=TARGET_PORT, timeout=1.2):
    """抓首页头部，确认是 iStoreOS/LuCI 页面。"""
    try:
        conn = http.client.HTTPConnection(ip, port, timeout=timeout)
        conn.request("GET", "/", headers={"User-Agent": "isto-finder/1.0"})
        r = conn.getresponse()
        data = r.read(8192).lower()
        conn.close()
        return any(mk in data for mk in FP_MARKERS)
    except Exception:
        return False


def mdns_probe():
    if not shutil.which("avahi-resolve"):
        return None
    for name in MDNS_NAMES:
        out = _sh(f"avahi-resolve -4 -n {name}", 8)
        m = re.search(r"(\d+\.\d+\.\d+\.\d+)", out)
        if not m:
            continue
        ip = m.group(1)
        if ip in local_ips():
            continue
        if not mac_ok_for(ip):
            continue
        if tcp_open(ip, TARGET_PORT, 0.6):
            return ip
    return None


def ping_sweep():
    """对物理/LAN 接口的子网做快速 ping 扫描，逼出 ARP 表。
    跳过 docker/virbr 等虚拟网桥。"""
    hosts = []
    out = _sh("ip -4 -o addr show scope global", 8)
    for line in out.splitlines():
        if re.search(r"\b(docker|br-|virbr|veth|vnet|tun|tap|lo)\S*", line):
            continue
        m = re.search(r"inet (\d+\.\d+\.\d+\.\d+/\d+)", line)
        if not m:
            continue
        try:
            net = ipaddress.ip_interface(m.group(1)).network
        except ValueError:
            continue
        if net.prefixlen <= 24:
            hosts.extend(str(h) for h in list(net.hosts())[:1024])
    if not hosts:
        return []
    def p(ip):
        try:
            subprocess.run(["ping", "-c", "1", "-W", "1", "-q", ip],
                           capture_output=True, timeout=3)
        except Exception:
            pass
    with ThreadPoolExecutor(max_workers=64) as ex:
        list(ex.map(p, hosts))
    return hosts


def httpfp_fallback():
    """ARP 表活跃主机里用 HTTP 指纹找 iStoreOS。"""
    skip = local_ips()
    cands = [ip for ip in arp_entries() if ip not in skip]
    def probe(ip):
        if not mac_ok_for(ip):
            return None
        return ip if tcp_open(ip, TARGET_PORT, 0.4) and http_fingerprint(ip) else None
    hits = []
    with ThreadPoolExecutor(max_workers=48) as ex:
        for r in ex.map(probe, cands):
            if r:
                hits.append(r)
    return hits[0] if hits else None


# ---- 寻踪之外的兜底：手动跳转地址 + 经串口在虚拟机里修网络 ----

def valid_v4(s):
    try:
        ipaddress.IPv4Address(s)
        return True
    except ValueError:
        return False


def manual_ip():
    """入口页手动记下的跳转地址；无效或未设置返回 None。"""
    try:
        with open(MANUAL_IP_FILE) as f:
            v = f.readline().strip()
    except OSError:
        return None
    return v if valid_v4(v) else None


def save_manual_ip(v):
    """空串表示清除，返回生效后的值（None 或地址）。"""
    try:
        os.makedirs(os.path.dirname(MANUAL_IP_FILE), exist_ok=True)
        if v:
            with open(MANUAL_IP_FILE, "w") as f:
                f.write(v + "\n")
        elif os.path.exists(MANUAL_IP_FILE):
            os.remove(MANUAL_IP_FILE)
    except OSError:
        return None
    return v or None


def guest_run(cmds, per=4, tail=900):
    """通过 libvirt 串口在虚拟机里执行命令——没有 IP 时这是唯一入口。

    用 script(1) 造一个 pty 挂住 virsh console，逐条喂命令，把屏幕内容读回来。
    命令只在本文件里写死；外部传入的东西必须先过 valid_v4 才拼得进来。
    """
    lines = ["sleep 2", "printf '\\n'", "sleep 1"]
    for c in cmds:
        lines.append("printf '%s\\n' " + shlex.quote(c))
        lines.append("sleep %d" % per)
    lines.append("sleep 3")
    total = 3 + per * len(cmds) + 20
    pipeline = ("{ " + "; ".join(lines) + "; } | timeout " + str(total) +
                " script -qec 'virsh -c qemu:///system console " + VM_NAME + "' /dev/null")
    try:
        p = subprocess.run(["bash", "-c", pipeline], capture_output=True,
                           text=True, timeout=total + 25)
    except Exception as exc:
        return "串口执行失败：" + str(exc)
    return ((p.stdout or "") + (p.stderr or ""))[-tail:]


def guest_dhcp_retry():
    """让 LAN 口重新走一遍 DHCP。

    千万别在这里手跑 `udhcpc -n -t 6 -i br-lan`：那是 netifd 托管的接口，
    第二个客户端退出时会给脚本发 deconfig，把地址和路由一起冲掉——实测
    静态地址口被这么搞一次就 `ping: Network unreachable`，虚拟机整个失联
    （只能 /etc/init.d/network restart 救回来）。所以只让 netifd 自己重来，
    再顺带请它已有的租约客户端续一次约（静态口没这个对象，报错也无妨）。
    """
    return guest_run(["ifup lan",
                      "ubus call network.interface.lan udhcpc renew 2>/dev/null || true",
                      "ip -4 a show " + GUEST_LAN_DEV,
                      "ip route"], per=7)


def guest_net_restart():
    """整机网络重启：治 netifd 状态错乱（地址在、路由没了这类）。"""
    return guest_run(["/etc/init.d/network restart",
                      "sleep 8",
                      "ip -4 a show " + GUEST_LAN_DEV,
                      "ip route"], per=9)


def guest_net_mode(mode, ip="", prefix="24", gw="", dns=""):
    """在虚拟机里把 LAN 口设为静态地址或恢复自动获取；参数非法返回 None。"""
    if mode == "static":
        try:
            mask = str(ipaddress.IPv4Network(ip + "/" + prefix, strict=False).netmask)
        except ValueError:
            return None
        cmds = ["uci set network.lan.proto='static'",
                "uci set network.lan.ipaddr='" + ip + "'",
                "uci set network.lan.netmask='" + mask + "'"]
        if gw:
            cmds.append("uci set network.lan.gateway='" + gw + "'")
        if dns:
            cmds.append("uci set network.lan.dns='" + dns + "'")
    else:
        cmds = ["uci set network.lan.proto='dhcp'",
                "uci -q del network.lan.ipaddr",
                "uci -q del network.lan.netmask",
                "uci -q del network.lan.gateway",
                "uci -q del network.lan.dns"]
    cmds += ["uci commit network", "ifup lan", "ip -4 a show " + GUEST_LAN_DEV]
    return guest_run(cmds, per=4)


# 串口操作要几十秒，HTTP 请求不能干等：放后台线程跑，入口页 3 秒一轮看进度
_net_lock = threading.Lock()
_net_job = {"running": False, "note": "", "out": "", "ts": 0.0}


def start_net_job(kind, args=(), note=""):
    with _net_lock:
        if _net_job["running"]:
            return False
        if not vm_running():
            _net_job.update(running=False, note="虚拟机没在运行，串口进不去", out="",
                            ts=time.time())
            return False
        _net_job.update(running=True, note=note, out="", ts=time.time())

    def worker():
        try:
            if kind == "dhcp":
                out = guest_dhcp_retry()
            elif kind == "dhcp-mode":
                out = guest_net_mode("dhcp")
            elif kind == "net-restart":
                out = guest_net_restart()
            else:
                out = guest_net_mode("static", *args)
        except Exception as exc:
            out = "执行异常：" + str(exc)
        with _net_lock:
            _net_job.update(running=False, note=note, out=(out or "")[-1200:])

    threading.Thread(target=worker, daemon=True).start()
    return True


def _resolve_locked():
    """真正干活的发现链，只在持有 _lock 时调用。返回 dict：
      ip      探活通过、可以直接跳过去的地址（没有就 None）
      source  发现层（mac+arp / mdns / domifaddr / sweep+arp / httpfp）
      stage   ok / web-warming（找到地址但 80 还没通）/ no-ip（开好了没地址）/
              stopped（虚拟机没开）/ booting（刚确认开着、地址还没查出来，
              由 resolve_ip 的开关机态校正临时给出，发现链不产出这个值）
      probing 探活没通过的那个候选地址，纯给排障看
      stale   该候选已经不应答了（ARP/租约残留），此时 stage 是 no-ip 不是 web-warming

    这条链在「没有地址」时最费时间（mDNS + domifaddr + 整段局域网 ping 扫描
    + HTTP 指纹，实测单次 10~16 秒），所以绝不直接在 HTTP 请求里调用它——
    入口页 5 秒一轮，全排队的话页面能拖到几十秒。统一走下面的 resolve_ip()。
      mac     虚拟机网卡 MAC，排障用
    以前只回 (ip, source)，于是「没开机」「开好了但没 IP」「有 IP 但 Web 没起」
    三种完全不同的情况在入口页都是同一句"正在启动"，出问题只能靠串口。
    """
    now = time.time()
    with _lock:
        # 手动指定过地址就优先用它（仍要探活），五层发现链救不了的场景兜底
        man = manual_ip()
        if man and tcp_open(man, TARGET_PORT):
            return {"ip": man, "source": "manual", "stage": "ok",
                    "probing": None, "stale": False, "mac": vm_mac()}
        if not vm_running():
            _cache["ip"] = None
            return {"ip": None, "source": None, "stage": "stopped",
                    "probing": None, "stale": False, "mac": vm_mac()}
        # 缓存命中：TCP 快速验证后直接采用
        if (_cache["ip"] and now - _cache["ts"] < CACHE_TTL
                and mac_ok_for(_cache["ip"])
                and tcp_open(_cache["ip"], TARGET_PORT)):
            return {"ip": _cache["ip"], "source": _cache["source"], "stage": "ok",
                    "probing": None, "stale": False, "mac": _cache["mac"]}
        mac = vm_mac()
        # 1) MAC → ARP
        ip = arp_lookup(mac)
        source = "mac+arp" if ip else None
        # 2) mDNS
        if not ip:
            ip = mdns_probe()
            source = "mdns" if ip else None
        # 3) virsh domifaddr
        if not ip:
            ip = domifaddr()
            source = "domifaddr" if ip else None
        # 4) ping 扫描后再查 ARP
        if not ip and now - _cache["sweep_ts"] > SWEEP_COOLDOWN:
            _cache["sweep_ts"] = now
            ping_sweep()
            ip = arp_lookup(mac)
            if ip:
                source = "sweep+arp"
        # 5) HTTP 指纹补齐
        if not ip:
            ip = httpfp_fallback()
            source = "httpfp" if ip else None
        probing, stale = None, False
        if ip and not tcp_open(ip, TARGET_PORT):
            # 端口没通不等于「Web 还在起」，要先看那地址上到底有没有机器：
            #   有人应答 → 系统起来了、Web 服务还没就绪 → web-warming（等就是了）
            #   没人应答 → ARP/租约里的残留地址 → no-ip（得让用户去手动处理）
            # 以前一律报 web-warming，于是路由器不发地址时入口能挂着
            # 「已找到 192.168.3.72，Web 服务还在起来」挂一整天，把排障带偏。
            # 注意：地址作废时 source 也得一起作废，否则 /api/status 会出现
            # {"ip": null, "source": "mac+arp"} 这种自相矛盾的排障线索。
            probing, ip, source = ip, None, None
            if not host_alive(probing) and not just_powered_on():
                stale = True
        if ip:
            _cache.update(ip=ip, mac=mac, ts=now, source=source)
            return {"ip": ip, "source": source, "stage": "ok",
                    "probing": None, "stale": False, "mac": mac}
        _cache["ip"] = None
        return {"ip": None, "source": None,
                "stage": "no-ip" if (stale or not probing) else "web-warming",
                "probing": probing, "stale": stale, "mac": mac}


# 发现结果由后台线程维护，HTTP 请求只读缓存：没地址时入口也能瞬间出画面。
# ask 是「上一次有人真的来看」的时间戳——没人看的时候把节奏放慢，别空转。
_last = {"res": None, "ts": 0.0, "ask": 0.0}
_kick = threading.Event()     # 有人发现开关机态变了，喊后台立刻跑一轮
DISCOVERY_INTERVAL = 5      # 有人在看：每 5 秒发现一次
DISCOVERY_IDLE = 20         # 没人看：降到每 20 秒


def _power_transition(on_now):
    """开关机态刚翻转时的过渡结论（只在请求线程里拼，绝不跑发现链）。

    关机方向可以当场断定（没什么可发现的），所以顺手把缓存也改对，
    入口页下一眼就是「已关机 + 启动虚拟机」；开机方向地址还不知道，
    先给中立的 booting（页面表现就是「正在启动」），地址交给后台那一轮。
    """
    if not on_now:
        res = {"ip": None, "source": None, "stage": "stopped", "probing": None,
               "stale": False, "mac": _cache.get("mac")}
        with _lock:
            _cache["ip"] = None
            _last.update(res=res, ts=time.time())
        return res
    return {"ip": None, "source": None, "stage": "booting", "probing": None,
            "stale": False, "mac": _cache.get("mac")}


def resolve_ip():
    """入口页与状态接口统一用它：瞬间返回后台发现线程维护的最新结论。

    冷启动（一次都还没跑过）才现算一次，避免首页空着。返回的结论最多旧一个
    发现周期（5 秒），跟入口页自己的轮询节奏一致，用户看不出差别——
    唯独开关机态例外：那个太便宜，值得每次跟虚拟机实际状态对一遍。
    """
    res = _last["res"]
    _last["ask"] = time.time()
    if res is None:
        with _lock:
            if _last["res"] is None:
                _last.update(res=_resolve_locked(), ts=time.time())
            res = _last["res"]
    else:
        on_now = vm_running_now()
        if (res.get("stage") != "stopped") != on_now:
            _kick.set()             # 让后台发现线程别等到下个周期
            res = _power_transition(on_now)
    return dict(res) if res else {"ip": None, "source": None, "stage": "no-ip",
                                  "probing": None, "stale": False, "mac": None}


def discovery_loop():
    """后台单飞：按节奏跑发现链，结果写进 _last 供所有请求读。"""
    while True:
        try:
            with _lock:
                res = _resolve_locked()
            _last.update(res=res, ts=time.time())
        except Exception:
            pass    # 单轮失败下一轮再来，别把线程搞死
        # 入口页开着时勤快点，没人看时省点 CPU；
        # 有人看见开关机态变了会 _kick.set()，这里立刻醒过来跑一轮
        gap = (DISCOVERY_INTERVAL
               if time.time() - _last["ask"] < 30 else DISCOVERY_IDLE)
        _kick.wait(gap)
        _kick.clear()


PAGE_TMPL = """<!DOCTYPE html><html lang="zh"><head><meta charset="utf-8">\
<meta name="viewport" content="width=device-width,initial-scale=1">\
$REFRESH<title>iStoreOS</title><style>\
*{box-sizing:border-box}body{font-family:system-ui,'PingFang SC','Microsoft YaHei',sans-serif;\
background:#0b1117;color:#e6edf3;display:flex;align-items:center;justify-content:center;\
min-height:100vh;margin:0;-webkit-font-smoothing:antialiased}\
.c{text-align:center;max-width:420px;padding:24px}\
.dot{width:10px;height:10px;border-radius:50%;background:#4b5b6b;display:block;margin:0 auto 22px}\
.dot.wait{background:#e8a03a;animation:br 1.6s ease-in-out infinite}\
@keyframes br{50%{opacity:.35}}\
h1{font-size:20px;font-weight:600;margin:0 0 10px;letter-spacing:.5px}\
p{color:#8296a8;font-size:14px;margin:0 0 24px;line-height:1.7}\
code{font-size:12px;color:#5f7385;background:#131c26;padding:2px 8px;border-radius:6px}\
button{font-size:15px;padding:11px 34px;border-radius:999px;border:0;background:#2563eb;\
color:#fff;cursor:pointer;letter-spacing:1px}button:hover{background:#3b76f0}\
button.alt{background:#1d2b38;color:#a9bccd}a{color:#4f8df9;text-decoration:none}\
\
.mini{color:#5f7385;font-size:12px;margin:0 0 14px;line-height:1.75;word-break:break-all}\
.row{display:flex;gap:10px;justify-content:center;align-items:center;flex-wrap:wrap;margin:0 0 12px}\
.row form{margin:0}\
button.sm{font-size:14px;padding:9px 20px;letter-spacing:.5px}\
details{max-width:340px;margin:0 auto 12px;border:1px solid #16222e;border-radius:14px;\
padding:0 14px;text-align:left}\
summary{font-size:13px;color:#8296a8;padding:12px 0;cursor:pointer;list-style:none;outline:none}\
summary::-webkit-details-marker{display:none}\
summary::before{content:"▸  ";color:#4f8df9}\
details[open] summary::before{content:"▾  "}\
details form{margin:4px 0 14px;display:flex;gap:8px;flex-wrap:wrap}\
details input,details select,details button{margin:0}\
details input{flex:1 1 118px;min-width:0}\
.sec{text-align:left;font-size:13px;color:#5f7385;margin:20px 0 10px;\
border-top:1px solid #16222e;padding-top:14px}\
form{margin:0 0 12px}input,select{font-size:14px;padding:10px 12px;border-radius:10px;\
border:1px solid #223140;background:#101a23;color:#e6edf3;margin:0 6px 10px 0}\
pre{font-size:12px;color:#8296a8;background:#101a23;padding:10px 12px;border-radius:10px;\
text-align:left;overflow:auto;white-space:pre-wrap}\
</style></head><body><div class="c">$BODY</div></body></html>"""


STATE_FILE = "/tmp/istoreos-install.state"
DISK_MARK_FILE = "/vol1/vm/istoreos.disk-version"


def os_version():
    """磁盘里的 iStoreOS 版本：装机时写的标记，退回虚拟机定义里的 osVersion。"""
    try:
        with open(DISK_MARK_FILE) as f:
            v = f.readline().strip()
            if v:
                return v
    except OSError:
        pass
    xml = _sh(f"virsh -c qemu:///system dumpxml {VM_NAME}", 8)
    m = re.search(r"<osVersion[^>]*>iStoreOS ([0-9.]+)", xml)
    return m.group(1) if m else None


def install_state():
    """后台安装进度（install_callback 秒回模式写入）；无文件返回 None。"""
    try:
        with open(STATE_FILE) as f:
            return f.read().strip()[:200] or None
    except OSError:
        return None


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = "IstoFinder/1.0"

    def _send(self, code, body=b"", ctype="text/html; charset=utf-8"):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def _page(self, title, msg, refresh=0, dot=""):
        rf = f'<meta http-equiv="refresh" content="{refresh}">' if refresh else ""
        body = f'<span class="dot {dot}"></span><h1>{title}</h1><p>{msg}</p>'
        html = PAGE_TMPL.replace("$REFRESH", rf).replace("$BODY", body)
        html = html.replace("$PORT", str(PORT)).replace("$TARGET", str(TARGET_PORT))
        return html.encode("utf-8")

    def _raw(self, body, refresh=0):
        rf = f'<meta http-equiv="refresh" content="{refresh}">' if refresh else ""
        html = PAGE_TMPL.replace("$REFRESH", rf).replace("$BODY", body)
        return html.encode("utf-8")

    def _net_page(self, note=""):
        """网络修复页：跟入口页同一套简洁风——默认只露「重新获取 IP」，
        静态地址、手动跳转地址、串口输出都折进可展开的小节，状态压成一行小字。
        页面不自动刷新（表单页刷新会把填一半的东西冲掉），
        只有串口任务在跑时才 3 秒轮一次。逻辑一行没动，改的只是版式。"""
        st = resolve_ip()
        man = manual_ip() or ""
        with _net_lock:
            job = dict(_net_job)
        esc = lambda s: (s or "").replace("&", "&amp;").replace("<", "&lt;")
        busy = job["running"]
        stage_cn = {"ok": "就绪", "web-warming": "Web 服务启动中",
                    "no-ip": "还在获取 IP", "stopped": "虚拟机未开机",
                    "booting": "系统启动中"}.get(st["stage"], st["stage"])
        p = ['<div><span class="dot%s"></span></div><h1>网络修复</h1>'
             % (" wait" if busy else ""),
             '<p class="mini">下面几步经 libvirt 串口在虚拟机里执行，它没有 IP 也能用。</p>']
        if note:
            p.append('<p class="mini"><code>%s</code></p>' % esc(note))
        if busy:
            p.append('<p class="mini">串口正在执行：%s，约需一分钟，页面会自动刷新。</p>'
                     % esc(job["note"]))
        p.append('<div class="row"><form method="post" action="/net/dhcp">'
                 '<button type="submit">重新获取 IP</button></form></div>')
        p.append('<div class="row">'
                 '<form method="post" action="/net/restart">'
                 '<button class="alt sm" type="submit">重启网络</button></form>'
                 '<form method="post" action="/net/dhcp-mode">'
                 '<button class="alt sm" type="submit">恢复自动获取</button></form></div>')
        p.append('<details><summary>路由器不发地址时：指定静态 IP</summary>'
                 '<form method="post" action="/net/static">'
                 '<input name="ip" placeholder="IP 192.168.3.72" inputmode="decimal">'
                 '<select name="prefix"><option value="24">/24</option>'
                 '<option value="16">/16</option><option value="8">/8</option></select>'
                 '<input name="gw" placeholder="网关（可留空）">'
                 '<input name="dns" placeholder="DNS（可留空）">'
                 '<button class="sm" type="submit">应用</button></form></details>')
        p.append('<details><summary>虚拟机在别的网段：手动指定跳转地址</summary>'
                 '<form method="post" action="/net/manual">'
                 '<input name="ip" placeholder="%s" inputmode="decimal">'
                 '<button class="sm" type="submit">保存</button></form>%s</details>'
                 % (esc(man) or "已知地址 192.168.1.1",
                    ('<p class="mini">当前 %s</p>'
                     '<form method="post" action="/net/manual">'
                     '<button class="alt sm" type="submit" name="clear" value="1">'
                     '清除手动地址</button></form>' % esc(man)) if man else ""))
        if not busy and job["out"]:
            p.append('<details><summary>串口输出</summary><pre>%s</pre></details>'
                     % esc(job["out"]))
        p.append('<p class="mini">%s · %s · %s</p>'
                 % (esc(stage_cn), esc(st["mac"] or "无网卡"),
                    esc(os_version() or "未知")))
        p.append('<p><a href="/">← 返回入口</a></p>')
        return self._raw("".join(p), refresh=3 if busy else 0)

    def do_GET(self):
        path = self.path
        pure = path.split("?")[0]
        if pure in ("/healthz", "/api/healthz"):
            self._send(200, b"ok", "text/plain")
            return
        if pure == "/api/status":
            r = resolve_ip()
            payload = json.dumps(
                # running 用同一份快照判断：早先是再调一次 vm_running()，
                # 与 stage 的来源不是同一次 virsh，出现过 stage=no-ip 而
                # running=false 的自相矛盾输出。
                {"running": r["stage"] != "stopped", "ip": r["ip"],
                 "source": r["source"], "stage": r["stage"], "probing": r["probing"],
                 "mac": r["mac"], "os_version": os_version(), "manual": manual_ip(),
                 "stale": bool(r.get("stale")),
                 "target_port": TARGET_PORT, "install_state": install_state()},
                ensure_ascii=False,
            ).encode("utf-8")
            self._send(200, payload, "application/json; charset=utf-8")
            return
        if pure == "/net":
            self._send(200, self._net_page())
            return
        if pure == "/power/start":
            # 开机接口只认 POST。浏览器停在 /power/start 时刷新、后退或
            # meta refresh 都会走到这里，若交给下面的透传逻辑就会被原样
            # 镜像成 http://<VM_IP>/power/start（虚拟机上没这个页面）。
            self.send_response(302)
            self.send_header("Location", "/")
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            return
        r = resolve_ip()
        if r["ip"]:
            tail = pure or "/"
            if "?" in path:
                tail += "?" + path.split("?", 1)[1]
            self.send_response(302)
            self.send_header("Location", f"http://{r['ip']}:{TARGET_PORT}{tail}")
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            return
        st = install_state()
        if st and st != "ready":
            self._send(200, self._page(
                "正在准备",
                f"首次安装约需 3~8 分钟<br><code>{st}</code>", refresh=5, dot="wait"))
            return
        if not vm_defined():
            self._send(200, self._page(
                "准备中", "正在等待应用完成安装。", refresh=5, dot="wait"))
            return
        if r["stage"] == "stopped":
            if os.path.exists(STOP_MARK):
                # 用户在应用中心点过「停用」：那就别擅自开机，给回 1.1.2 那套
                # 简洁关机页 + 一键开机按钮（点了才开，开了自动跳）。
                btn = ('<form method="POST" action="/power/start">'
                       '<button type="submit">启动虚拟机</button></form>')
                self._send(200, self._page("iStoreOS 已关机", btn, refresh=3))
                return
            # 不是用户关的（应用重启、虚拟机里手动关机等）：打开入口就顺手补开机。
            # 应用中心的「启用」不会回调应用脚本（实测 cmd/main 只收到 stop）。
            vm_power_on()
            self._send(200, self._page(
                "正在启动", "iStoreOS 就绪后会自动打开。", refresh=3, dot="wait"))
            return
        if r["stage"] == "booting":
            # 刚确认虚拟机开着、地址还等后台那一轮：给中立的「正在启动」，
            # 这时说「路由器 DHCP 没发地址」为时过早。
            self._send(200, self._page(
                "正在启动", "iStoreOS 就绪后会自动打开。", refresh=3, dot="wait"))
            return
        if r["stage"] == "web-warming":
            self._send(200, self._page(
                "正在启动",
                f"已找到 <code>{r['probing']}</code>，iStoreOS 的 Web 服务还在起来。",
                refresh=5, dot="wait"))
            return
        # 虚拟机开着却始终没有可用地址：现实里真发生过——链路通、IPv6 都拿到了，
        # 但 LAN 上没有 DHCPv4 服务器应答。这时不能只让用户干等，给出串口修复入口。
        residue = (f"<br>ARP 里还留着 <code>{r['probing']}</code>，"
                   f"但它现在不应答，是上次开机留下的。"
                   if r.get("stale") and r.get("probing") else "")
        self._send(200, self._page(
            "正在获取地址",
            f"虚拟机已开机，iStoreOS 还没拿到 IPv4（网卡 <code>{r['mac'] or '未知'}</code>）。"
            f"{residue}<br>通常是路由器 DHCP 没发地址；等不下去可以"
            f"<a href=\"/net\">手动处理</a>（重试 DHCP / 设静态地址）。",
            refresh=5, dot="wait"))

    def do_POST(self):
        pure = self.path.split("?")[0]
        raw = b""
        try:
            length = int(self.headers.get("Content-Length") or 0)
            if length:
                raw = self.rfile.read(length)
        except ValueError:
            pass
        form = urllib.parse.parse_qs(raw.decode("utf-8", "replace"))
        field = lambda k: (form.get(k) or [""])[0].strip()

        if pure == "/power/start":
            res = vm_power_on()
            msg = {"starting": "iStoreOS 就绪后会自动打开。",
                   "resumed": "iStoreOS 就绪后会自动打开。",
                   "already-running": "iStoreOS 已在运行。",
                   "undefined": "正在等待应用完成安装。"}[res]
            # 刷新必须显式回到 "/"：只写 content="5" 会拿当前 URL
            # （/power/start）重新 GET，等于把控制路径跳给虚拟机。
            self._send(200, self._page("正在启动", msg, refresh="5;url=/", dot="wait"))
            return

        if pure == "/net/dhcp":
            ok = start_net_job("dhcp", note="ifup lan + 请 netifd 续租")
            self._send(200, self._net_page("" if ok else "上一个串口任务还没结束，稍等一下"))
            return
        if pure == "/net/dhcp-mode":
            ok = start_net_job("dhcp-mode", note="把 LAN 口改回自动获取")
            self._send(200, self._net_page("" if ok else "上一个串口任务还没结束，稍等一下"))
            return
        if pure == "/net/restart":
            ok = start_net_job("net-restart", note="重启虚拟机网络")
            self._send(200, self._net_page("" if ok else "上一个串口任务还没结束，稍等一下"))
            return
        if pure == "/net/static":
            ip, gw, dns, prefix = field("ip"), field("gw"), field("dns"), field("prefix") or "24"
            bad = [v for v in (ip, gw, dns) if v and not valid_v4(v)]
            if not valid_v4(ip) or bad or prefix not in ("8", "16", "24"):
                self._send(200, self._net_page("地址不合法：请填写正确的 IPv4（网关/DNS 可留空）"))
                return
            ok = start_net_job("static", args=(ip, prefix, gw, dns),
                               note=f"设静态地址 {ip}/{prefix}")
            self._send(200, self._net_page("" if ok else "上一个串口任务还没结束，稍等一下"))
            return
        if pure == "/net/manual":
            if field("clear") == "1":
                save_manual_ip("")
                self._send(200, self._net_page("已清除手动跳转地址。"))
                return
            v = field("ip")
            if v and not valid_v4(v):
                self._send(200, self._net_page("地址不合法：请填写正确的 IPv4"))
                return
            save_manual_ip(v)
            self._send(200, self._net_page(
                f"手动跳转地址已{'保存，探活通过就会直接跳过去' if v else '清除'}"
                f'{": " + v if v else ""}。'))
            return
        self._send(404, b"not found", "text/plain")

    do_HEAD = do_GET

    def log_message(self, fmt, *args):
        pass

    def handle_error(self, request, client_address):
        """入口页每 5 秒轮询，浏览器经常提前掐线；这种 ConnectionResetError
        不是故障，别把它刷进 journal。"""
        exc = sys.exc_info()[1]
        if isinstance(exc, (ConnectionResetError, BrokenPipeError)):
            return
        super().handle_error(request, client_address)


def main():
    httpd = http.server.ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    httpd.daemon_threads = True
    threading.Thread(target=discovery_loop, daemon=True).start()
    print(f"[istoreos-web] {PORT} finder -> vm '{VM_NAME}' :{TARGET_PORT}")
    httpd.serve_forever()


if __name__ == "__main__":
    main()

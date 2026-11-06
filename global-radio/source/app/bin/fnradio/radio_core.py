"""
fnradio —— 轻量「全球电台 → 飞牛音乐」中继的核心逻辑。

不依赖 fnmusic-ext，只做 radio 一件事：
  - 搜 radio-browser.info 全网电台
  - 把电台伪装成飞牛音乐的"在线歌曲"（guid = online:radio:<stationuuid>）
  - 下发时把 guid 换成官方 32-hex 形态，播放时反解回真实 guid 取直播流

guid 伪装机制（对齐官方：客户端按 32-hex 过滤，非 hex 的 online: 前缀会被整条丢弃）：
  fake = md5("fnradio::" + real_guid).hexdigest()   # 确定性映射，可反解
"""
import asyncio
import hashlib
import json
import re
import socket
import time as _time_mod

import httpx

# radio-browser.info 镜像 host（多节点，任一可达即可）。
# 注意：本机 IPv6 出站黑洞（httpx 默认 happy-eyeballs 优先 IPv6 → ReadError），
# 故强制解析 IPv4 后用 IP 直连 + Host header 请求（HTTP 80）。
# at1/nl1 域名已下线（Name or service not known），已移除。
RADIO_HOSTS = [
    "de1.api.radio-browser.info",
    "all.api.radio-browser.info",
]

_ipv4_cache: dict[str, str] = {}


def _ipv4(host: str) -> str:
    """解析 host 的 IPv4 地址（强制 AF_INET，避开 IPv6 黑洞），失败返回空串。"""
    if host not in _ipv4_cache:
        try:
            _ipv4_cache[host] = socket.getaddrinfo(
                host, 80, socket.AF_INET, socket.SOCK_STREAM)[0][4][0]
        except Exception:
            _ipv4_cache[host] = ""
    return _ipv4_cache[host]


# radio-browser 限流节流：所有请求共享最小间隔，避免突发（预热/连点）触发封 IP
_rate_lock = asyncio.Lock()
_rate_last = 0.0
_RATE_INTERVAL = 0.4  # 秒


async def _throttle() -> None:
    global _rate_last
    async with _rate_lock:
        now = _time_mod.monotonic()
        wait = _RATE_INTERVAL - (now - _rate_last)
        if wait > 0:
            await asyncio.sleep(wait)
        _rate_last = _time_mod.monotonic()

RADIO_TAG = "[电台] "
GUID_PREFIX = "online:radio:"

# 常见国家英文名 -> 中文，用于 artist 显示（无匹配则原样保留英文）
_COUNTRY_ZH = {
    "Germany": "德国", "United States": "美国", "United Kingdom": "英国",
    "France": "法国", "Italy": "意大利", "Spain": "西班牙", "Portugal": "葡萄牙",
    "Netherlands": "荷兰", "Belgium": "比利时", "Switzerland": "瑞士",
    "Austria": "奥地利", "Poland": "波兰", "Czech Republic": "捷克",
    "Russia": "俄罗斯", "Ukraine": "乌克兰", "Sweden": "瑞典", "Norway": "挪威",
    "Finland": "芬兰", "Denmark": "丹麦", "Ireland": "爱尔兰", "Greece": "希腊",
    "Turkey": "土耳其", "Japan": "日本", "South Korea": "韩国", "China": "中国",
    "India": "印度", "Australia": "澳大利亚", "Canada": "加拿大", "Brazil": "巴西",
    "Mexico": "墨西哥", "Argentina": "阿根廷", "Chile": "智利",
    "Taiwan": "台湾", "Hong Kong": "香港", "Macau": "澳门", "Macao": "澳门",
    "Singapore": "新加坡", "Malaysia": "马来西亚", "Thailand": "泰国",
    "Indonesia": "印度尼西亚", "Vietnam": "越南", "Philippines": "菲律宾",
    "South Africa": "南非", "Egypt": "埃及", "Israel": "以色列",
    "Saudi Arabia": "沙特阿拉伯", "New Zealand": "新西兰",
    "Pakistan": "巴基斯坦", "Bangladesh": "孟加拉国",
}


def _country_label(country: str) -> str:
    """国家英文名转中文标签，含常见变体模糊匹配，无匹配原样返回。"""
    if country in _COUNTRY_ZH:
        return _COUNTRY_ZH[country]
    c = country.lower()
    if "united states" in c or c in ("usa", "us"):
        return "美国"
    if "united kingdom" in c or c == "uk":
        return "英国"
    if "south korea" in c or c == "korea":
        return "韩国"
    if "north korea" in c or ("korea" in c and "democratic" in c):
        return "朝鲜"
    if "republic of korea" in c:
        return "韩国"
    if "russia" in c:
        return "俄罗斯"
    if "czech" in c:
        return "捷克"
    if "taiwan" in c:
        return "台湾"
    if "hong kong" in c or c == "hk":
        return "香港"
    if "macau" in c or "macao" in c or c == "mo":
        return "澳门"
    if "singapore" in c:
        return "新加坡"
    if "malaysia" in c:
        return "马来西亚"
    if "thailand" in c:
        return "泰国"
    if "indonesia" in c:
        return "印度尼西亚"
    if "viet nam" in c or "vietnam" in c:
        return "越南"
    if "philippines" in c:
        return "菲律宾"
    return country


def _clean_station_name(name: str) -> str:
    """清理电台名自带的格式后缀（如 "Dance Wave! | MP3" -> "Dance Wave!"）。"""
    cleaned = re.sub(r"\s*[|/]\s*(MP3|AAC\+?|AAC|FLAC|OGG|OPUS|HLS|HTTP|HE-AAC|M3U8?)\s*$",
                     "", name, flags=re.IGNORECASE).strip()
    return cleaned or name.strip()

# 反向映射 fake(32-hex) -> real guid，进程内存维护；fake 是 md5 确定值，重算也行
_fake_reverse: dict[str, str] = {}

# 已知 stationuuid 集合：resolve_guid 在映射 miss 时对它们重算 md5 反推
_known_sids: set[str] = set()

# stationuuid -> favicon URL 映射：static/cover 拦截时代理电台台标用（进程内存）
_favicon_map: dict[str, str] = {}


def favicon_for(sid: str) -> str:
    """按 stationuuid 反查电台台标 URL；miss 返回空串。"""
    return _favicon_map.get(str(sid), "")


def _md5_guid(real_guid: str) -> str:
    return hashlib.md5(f"fnradio::{real_guid}".encode()).hexdigest()


def fake_guid(real_guid: str) -> str:
    """real guid -> 官方 32-hex 形态（确定性）。"""
    fake = _md5_guid(real_guid)
    _fake_reverse.setdefault(fake, real_guid)
    return fake


def register_sid(sid: str) -> None:
    """登记 stationuuid，供 resolve_guid 反推。"""
    _known_sids.add(sid)


def resolve_guid(candidate: str) -> str:
    """32-hex -> real guid（online:radio:xxx）；非映射值原样返回。"""
    if candidate in _fake_reverse:
        return _fake_reverse[candidate]
    # 映射 miss：对已知 sid 重算 md5 反推（覆盖 station 文件注入 / 跨进程场景）
    if candidate and len(candidate) == 32:
        for sid in list(_known_sids):
            if _md5_guid(GUID_PREFIX + sid) == candidate:
                return GUID_PREFIX + sid
    return candidate


def radio_guid(stationuuid: str) -> str:
    return GUID_PREFIX + stationuuid


def _ext_for(station: dict) -> str:
    codec = str(station.get("codec") or "").upper()
    if "AAC" in codec or "AAC+" in codec or "HE-AAC" in codec:
        return "aac"
    if "OGG" in codec:
        return "ogg"
    if "FLAC" in codec:
        return "flac"
    return "mp3"


def station_to_track(station: dict) -> dict:
    """radio-browser 电台条目 -> 飞牛音乐在线 track VO。

    对齐飞牛前端 ZQ 解构 / _h() 期望：artists、album 对象、audioSpec、duration(毫秒)。
    直播流：duration=0、hasLyric=False、无 seek。
    """
    sid = str(station.get("stationuuid") or "")
    name = _clean_station_name(str(station.get("name") or "未知电台"))
    register_sid(sid)
    real_guid = radio_guid(sid)
    guid = fake_guid(real_guid)
    ext = _ext_for(station)
    tags = str(station.get("tags") or "").strip()
    country = str(station.get("country") or "").strip()
    codec = str(station.get("codec") or "MP3")
    bitrate = int(station.get("bitrate") or 0)
    favicon = str(station.get("favicon") or "")
    url_resolved = str(station.get("url_resolved") or station.get("url") or "")
    # 记录 favicon 供 static/cover 代理（电台台标）
    if favicon:
        _favicon_map[sid] = favicon
    # 信息增强：artist 用国家（中文标签），无国家兜底中文「网络电台」
    artist = _country_label(country) if country else "网络电台"
    title = RADIO_TAG + name
    # album 名附带码率/格式，列表里能看到具体信息
    album_name = f"{name} · {codec} {bitrate}kbps" if bitrate else name

    artists_list = [{"name": artist, "guid": f"{guid}:artist"}]
    album_obj = {
        "name": album_name,
        "guid": f"{guid}:album",
        "artists": artists_list,
        "coverId": "radio-" + sid,
    }
    audio_spec = {
        "path": f"online/radio/{sid}.{ext}",
        "format": ext,
        "codec": ext,
        "container": ext,
        "duration": 0,
        "size": 0,
        "channel": 2,
        "sampleRate": 44100,
        "bitrate": bitrate if bitrate else 128000,
    }
    return {
        "guid": guid,
        "id": guid,
        "title": title,
        "name": title,
        "artist": artist,
        "artists": artists_list,
        "album": album_obj,
        "albumName": album_name,
        "audioSpec": audio_spec,
        "duration": 0,
        "duration_ms": 0,
        "durationMs": 0,
        "codec": ext,
        "codecName": ext,
        "format": ext,
        "ext": ext,
        "size": 0,
        "file_size": 0,
        "coverId": "radio-" + sid,
        "cover_url": favicon,
        "coverUrl": favicon,
        "coverURL": favicon,
        "source": "radio",
        "is_online": True,
        "isFavorite": False,
        "isCue": False,
        "hasLyric": False,
        "genres": [{"name": g.strip()} for g in re.split(r"[, ]+", tags) if g.strip()] if tags else [],
        "accessStatus": 0,
        # 电台直播流元数据（供 stream 反查用）
        "_stream_url": url_resolved,
        "_real_guid": real_guid,
        "_codec_name": codec,
    }


def _station_stream_url(station: dict) -> str:
    return str(station.get("url_resolved") or station.get("url") or "")


def force_ipv4_url(url: str) -> tuple[str, dict]:
    """把 HTTP 流 URL 的 host 替换为 IPv4 IP，返回 (新 URL, 额外 headers)。

    中继 `_proxy_live_stream` 与 `_radio_get` 一样需要避开本机 IPv6 出站黑洞；
    HTTP 流用 IP 直连 + Host header（证书无关，安全）。HTTPS 流保持原样（httpx
    自行处理，避免 IP 直连破坏 SNI/证书校验）。解析失败或已无 IPv4 则原样返回。
    """
    from urllib.parse import urlparse, urlunparse
    try:
        p = urlparse(url)
        host = p.hostname
        if not host or p.scheme != "http":
            return url, {}
        ip = _ipv4(host)
        if not ip or ip == host:
            return url, {}
        port = p.port
        netloc = f"{ip}:{port}" if port else ip
        new_url = urlunparse((p.scheme, netloc, p.path, p.params, p.query, p.fragment))
        return new_url, {"Host": host}
    except Exception:
        return url, {}


def stream_host(url: str) -> str:
    """提取流 URL 的 host（仅主机名小写，无端口），用于连通性负缓存去重。"""
    from urllib.parse import urlparse
    try:
        return (urlparse(url).hostname or "").lower()
    except Exception:
        return ""


def host_reachable(url: str, timeout: float = 4.0) -> bool:
    """判断 NAS 能否真正连上流服务器（TCP + TLS + HTTP 响应头都通才算能连）。

    有些被墙的境外流媒体 TCP/TLS 握手能成，但 HTTP 请求发出后无响应（应用层被墙），
    因此必须收到 HTTP 响应头才算「能连」，纯 TCP connect 会漏掉这类（如 walmradio:8443）。
    强制 IPv4 直连避开本机 IPv6 出站黑洞。
    """
    import http.client
    import ssl
    from urllib.parse import urlparse
    try:
        p = urlparse(url)
        host = p.hostname
        port = p.port or (443 if p.scheme == "https" else 80)
        path = (p.path or "/") + (("?" + p.query) if p.query else "")
    except Exception:
        return False
    if not host:
        return False
    ip = _ipv4(host)
    target = ip or host
    conn = None
    try:
        sock = socket.create_connection((target, port), timeout=timeout)
        if p.scheme == "https":
            ctx = ssl.create_default_context()
            conn = http.client.HTTPSConnection(host, port, timeout=timeout, context=ctx)
            conn.sock = ctx.wrap_socket(sock, server_hostname=host)
        else:
            conn = http.client.HTTPConnection(host, port, timeout=timeout)
            conn.sock = sock
        conn.request("GET", path, headers={"Range": "bytes=0-1023", "User-Agent": "fnradio/1.0"})
        resp = conn.getresponse()
        _ = resp.status  # 收到 HTTP 响应头即算「能连」
        return True
    except Exception:
        return False
    finally:
        if conn is not None:
            try:
                conn.close()
            except Exception:
                pass


async def _radio_get(path: str, params: dict | None = None, timeout: float = 12.0) -> list:
    """IPv4 直连 + Host header 请求 radio-browser，返回原始 JSON list（失败返回 []）。"""
    for host in RADIO_HOSTS:
        ip = _ipv4(host)
        if not ip:
            continue
        await _throttle()  # 节流防限流
        try:
            async with httpx.AsyncClient(timeout=timeout, trust_env=False) as client:
                resp = await client.get(f"http://{ip}{path}", params=params,
                                        headers={"Host": host, "User-Agent": "fnradio/1.0"})
                if resp.status_code == 200:
                    data = resp.json()
                    if isinstance(data, list):
                        return data
        except Exception:
            continue
    return []


async def _fetch_stations(path: str, params: dict | None = None) -> list[dict]:
    """通用拉取 radio-browser 电台列表（过滤出含 stationuuid 的条目）。"""
    return [s for s in (await _radio_get(path, params))
            if isinstance(s, dict) and s.get("stationuuid")]


async def hot_stations(limit: int = 1000) -> list[dict]:
    """拉热门电台（topvote，按投票降序），无关键词时的默认电台列表。

    用 topvote 接口（独立限流池，比 /json/stations 通用接口更宽松、更稳）。
    topvote 天然按投票排序，热门台多数是活的，无需 hidebroken。
    """
    if limit <= 0:
        return []
    return await _fetch_stations(f"/json/stations/topvote/{limit}")


async def country_stations(countrycode: str, limit: int = 200) -> list[dict]:
    """按国家码拉电台（bycountrycodeexact，按 votes 降序 + 过滤死链）。"""
    if not countrycode:
        return []
    return await _fetch_stations(
        f"/json/stations/bycountrycodeexact/{countrycode}",
        {"limit": limit, "hidebroken": "true", "order": "votes", "reverse": "true"},
    )


async def list_countries() -> list[dict]:
    """拉国家列表（含 stationcount，用于挑热门国家建分类歌单）。"""
    data = await _radio_get("/json/countries")
    return [c for c in data if isinstance(c, dict) and c.get("iso_3166_1")]


async def list_tags() -> list[dict]:
    """拉标签列表（含 stationcount，用于挑热门标签建分类歌单）。"""
    data = await _radio_get("/json/tags")
    return [t for t in data if isinstance(t, dict) and t.get("name")]


async def tag_stations(tag: str, limit: int = 200) -> list[dict]:
    """按标签拉电台（search 接口 + tag 过滤，按 votes 降序 + 过滤死链）。"""
    if not tag:
        return []
    return await _fetch_stations(
        "/json/stations/search",
        {"tag": tag, "limit": limit, "hidebroken": "true", "order": "votes", "reverse": "true"},
    )


async def search_stations(keyword: str, limit: int = 20) -> list[dict]:
    """搜 radio-browser.info，返回电台列表（原始条目）。"""
    if not keyword:
        return []
    return await _fetch_stations(
        "/json/stations/search",
        {
            "name": keyword,
            "limit": limit,
            "hidebroken": "true",
            "order": "votes",
            "reverse": "true",
        },
    )


async def station_by_uuid(sid: str) -> dict | None:
    """按 stationuuid 查单条电台（内存 miss 时兜底）。"""
    data = await _radio_get(f"/json/stations/byuuid/{sid}")
    if data and isinstance(data[0], dict):
        return data[0]
    return None


def search_to_tracks(stations: list[dict]) -> list[dict]:
    """电台列表 -> 飞牛 track VO 列表。"""
    return [station_to_track(s) for s in stations]


def lookup_stream_url(real_guid: str, stations: dict[str, dict]) -> str | None:
    """按 real guid 反查直播流 URL。stations 为 {stationuuid: station} 缓存。"""
    if not real_guid.startswith(GUID_PREFIX):
        return None
    sid = real_guid[len(GUID_PREFIX):]
    st = stations.get(sid)
    if not st:
        return None
    return _station_stream_url(st)

"""
fnradio —— 轻量「全球电台 → 飞牛音乐」中继。

原理（对齐 javycoder/fnos_music_ext，但只做 radio）：
  - 接管官方 /var/run/trim_music.socket，自己绑定原路径
  - 其余请求原样透传官方 upstream（带鉴权头 cookie/authorization/x-trim-music-temp-token/authx）
  - 拦截 /music/api/v1/search/track：透传官方本地结果 + 追加 radio-browser 电台
  - 拦截 /music/api/v1/track/stream：guid 反解出 online:radio:<uuid> 时取直播流，其余透传

运行：uvicorn radio_relay:app --uds /var/run/trim_music.socket
"""
import asyncio
import json
import logging
import os
import re
from typing import AsyncGenerator

from urllib.parse import quote

import httpx
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, RedirectResponse, Response, StreamingResponse
from starlette.requests import ClientDisconnect

import radio_core as rc
import radio_cover as cover

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
logger = logging.getLogger("fnradio")

UPSTREAM_SOCK = os.environ.get("FNRADIO_UPSTREAM_SOCK", "/var/run/trim_music_upstream.socket")

HOP_BY_HOP = {
    "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
    "te", "trailers", "transfer-encoding", "upgrade",
}

AUTH_HEADERS = ("cookie", "authorization", "x-trim-music-temp-token", "authx")

app = FastAPI()

# 电台缓存 {stationuuid: station dict}，搜索时写入，stream 反查用（进程内）
_stations: dict[str, dict] = {}

# 可选 station 覆盖文件：JSON {stationuuid: {url_resolved, name, codec, ...}}，
# 用于手动指定电台流 URL / 测试注入。stream 时优先于 radio-browser 反查。
STATION_FILE = os.environ.get("FNRADIO_STATION_FILE", "")

# 「全球电台」虚拟歌单：纯 API 层注入，不落官方库。guid 固定，前端点它走 playlist 详情。
VIRTUAL_PLAYLIST_GUID = "global-radio"      # 主歌单（热门电台）
VIRTUAL_PLAYLIST_NAME = "全球电台"
HOT_PLAYLIST_NAME = "全球电台 · 热门"        # 主歌单卡片/详情显示名（与国家歌单「· 国名」对齐）
COUNTRY_GUID_PREFIX = "global-radio-"       # 国家分类歌单前缀（后接 ISO 两位码）
HOT_LIMIT = 1000                            # 主歌单热门电台上限（Q2 提上限）
COUNTRY_TOP_N = int(os.environ.get("FNRADIO_COUNTRY_TOP_N", "12"))  # 国家分类歌单数量（Q3）
COUNTRY_LIMIT = 200                         # 每个国家歌单电台上限
# 始终建分类歌单的国家/地区（ISO 码，即使电台数少也保留），中文用户常听
PINNED_COUNTRIES = {
    "CN", "TW", "HK", "MO", "JP", "KR", "SG", "MY", "TH", "ID", "VN", "PH", "IN",
}
HOT_COVER_ID = "radio-hot"                  # 主歌单（热门电台）封面
FLAG_COVER_PREFIX = "radio-flag-"           # 国家歌单封面前缀（后接 ISO 码）
FLAGCDN = "https://cdn.jsdelivr.net/gh/hatscripts/circle-flags/flags"  # 圆形国旗源（jsdelivr，国内可达，替代被墙的 flagcdn）
TAG_TOP_N = int(os.environ.get("FNRADIO_TAG_TOP_N", "24"))  # 标签分类歌单数量
TAG_LIMIT = 200                         # 每个标签歌单电台上限
TAG_GUID_PREFIX = "global-radio-tag-"   # 标签歌单 guid 前缀（后接标签名）
TAG_COVER_ID = "radio-tag"              # 标签歌单封面 id
TAG_MIN_COUNT = 30                      # 标签最少电台数（低于此不建歌单，避免垃圾/空歌单）

# 始终建分类歌单的流派白名单（标签名 -> 中文显示名）。radio-browser 的 /json/tags
# 被年代/FM频率/垃圾标签污染，核心音乐流派 stationcount 低、挤不进 top N，
# 故按白名单固定建歌单，数据源为按标签搜索（search?tag=<流派>）。
PINNED_TAGS = {
    "jazz": "爵士", "classical": "古典", "rock": "摇滚", "pop": "流行",
    "news": "新闻", "talk": "谈话", "electronic": "电子", "dance": "舞曲",
    "country": "乡村", "metal": "金属", "blues": "布鲁斯", "reggae": "雷鬼",
    "latin": "拉丁", "hip hop": "嘻哈", "rap": "说唱", "folk": "民谣",
    "ambient": "氛围", "soul": "灵魂", "funk": "放克", "gospel": "福音",
    "christian": "基督教", "oldies": "怀旧", "world": "世界音乐",
    "opera": "歌剧", "salsa": "萨尔萨", "edm": "电子舞曲", "indie": "独立",
    "punk": "朋克",
}

# 无台标电台的默认图标（PNG：深蓝圆角底 + 白色信号波纹），懒生成缓存
_default_cover_png: bytes | None = None


def _default_cover() -> bytes:
    global _default_cover_png
    if _default_cover_png is None:
        _default_cover_png = cover.default_cover_png()
    return _default_cover_png


# 实际电台数缓存：预热/详情返回时更新，卡片 trackCount 用它，避免写死
_cached_radio_count = 0

# 热门电台列表缓存：缓存 5 分钟，避免每次请求/预热并发打 radio-browser 被限流
_cached_stations: list = []
_cached_stations_ts: float = 0.0
_STATIONS_TTL = 300.0

# 连通性负缓存：{stream host: 失败时间戳}。播放失败或后台预检连不上时记录，
# 列表返回时过滤掉这些被墙的境外流媒体，避免「点了没声 → 播放器连续跳过」。
_conn_fail: dict[str, float] = {}
_CONN_FAIL_TTL = 900.0  # 15 分钟，网络波动后自动重试
# 连通性正缓存：{stream host: 成功时间戳}，避免后台预检反复测能连的 host
_conn_ok: dict[str, float] = {}
_CONN_OK_TTL = 1800.0  # 30 分钟
# 后台预检是否已完成一轮（完成后列表过滤才生效，避免首屏过滤滞后）
_conn_warmed = False

# 国家分类歌单缓存：
#   _countries: 国家列表 [{iso_3166_1, name, stationcount}]，启动预热，长 TTL
#   _country_stations: {countrycode: list}，按需拉取，5 分钟 TTL
_countries: list = []
_countries_ts: float = 0.0
_COUNTRIES_TTL = 86400.0
_country_stations: dict = {}
_country_stations_ts: dict = {}


async def _probe_url(url: str) -> bool:
    """异步包装 rc.host_reachable（线程池），避免阻塞事件循环。"""
    import asyncio
    return await asyncio.to_thread(rc.host_reachable, url, 4.0)


async def _prewarm_connectivity(stations: list) -> None:
    """后台并发预检热门电台流连通性，填充正/负缓存（限制并发，不阻塞请求）。"""
    global _conn_warmed
    import asyncio
    import time as _time
    now = _time.time()
    hosts: dict[str, str] = {}
    for st in stations:
        url = rc._station_stream_url(st)
        host = rc.stream_host(url)
        if not host:
            continue
        if now - _conn_ok.get(host, 0.0) < _CONN_OK_TTL:
            continue
        if now - _conn_fail.get(host, 0.0) < _CONN_FAIL_TTL:
            continue
        hosts.setdefault(host, url)
    if not hosts:
        _conn_warmed = True
        return
    sem = asyncio.Semaphore(30)

    async def probe(host: str, url: str) -> None:
        async with sem:
            ok = await _probe_url(url)
            t = _time.time()
            if ok:
                _conn_ok[host] = t
                _conn_fail.pop(host, None)
            else:
                _conn_fail[host] = t
                _conn_ok.pop(host, None)

    await asyncio.gather(*(probe(h, u) for h, u in hosts.items()))
    _conn_warmed = True


def _filter_dead_stations(stations: list) -> list:
    """过滤已知连不上的电台（按 stream host 负缓存）。"""
    import time as _time
    now = _time.time()
    for h in [h for h, ts in _conn_fail.items() if now - ts >= _CONN_FAIL_TTL]:
        _conn_fail.pop(h, None)
    out = []
    for st in stations:
        host = rc.stream_host(rc._station_stream_url(st))
        if host and host in _conn_fail:
            continue
        out.append(st)
    return out


async def _get_radio_stations() -> list:
    """带缓存的热门电台列表（进程内 5 分钟 TTL），返回时过滤已知被墙电台。"""
    global _cached_stations, _cached_stations_ts, _cached_radio_count
    import time as _time
    now = _time.time()
    if _cached_stations and (now - _cached_stations_ts) < _STATIONS_TTL:
        return _filter_dead_stations(_cached_stations)
    stations = await rc.hot_stations(limit=HOT_LIMIT)
    if stations:
        _cached_stations = stations
        _cached_stations_ts = now
        _cached_radio_count = len(stations)
    return _filter_dead_stations(stations)


async def _get_countries() -> list:
    """带缓存的国家列表（一天 TTL）：stationcount 前 COUNTRY_TOP_N + 白名单国家。"""
    global _countries, _countries_ts
    import time as _time
    now = _time.time()
    if _countries and (now - _countries_ts) < _COUNTRIES_TTL:
        return _countries
    allc = await rc.list_countries()
    if allc:
        allc.sort(key=lambda c: int(c.get("stationcount") or 0), reverse=True)
        top = allc[:COUNTRY_TOP_N]
        # 补充白名单国家（去重，保持 stationcount 降序追加）
        pinned = [c for c in allc
                  if str(c.get("iso_3166_1") or "").upper() in PINNED_COUNTRIES
                  and c not in top]
        _countries = top + pinned
        # 中国置顶（其余保持 stationcount 降序，sort 稳定不破坏顺序）
        _countries.sort(key=lambda c: 0 if str(c.get("iso_3166_1") or "").upper() == "CN" else 1)
        _countries_ts = now
    return _countries


async def _get_country_stations(countrycode: str) -> list:
    """按国家码拉电台（带缓存 5 分钟 TTL）。"""
    import time as _time
    now = _time.time()
    cached = _country_stations.get(countrycode)
    ts = _country_stations_ts.get(countrycode, 0.0)
    if cached and (now - ts) < _STATIONS_TTL:
        return cached
    stations = await rc.country_stations(countrycode, limit=COUNTRY_LIMIT)
    if stations:
        _country_stations[countrycode] = stations
        _country_stations_ts[countrycode] = now
    return stations

# 标签分类歌单缓存：_tags 为过滤后的热门标签列表（一天 TTL），_tag_stations_cache 按需拉（5 分钟 TTL）
_tags: list = []
_tags_ts: float = 0.0
_TAGS_TTL = 86400.0
_tag_stations_cache: dict = {}
_tag_stations_ts: dict = {}

# 标签过滤：排除 codec / 频率 / 电台名 / 纯数字垃圾标签，年代变体归一化去重
_CODEC_TAGS = {"aac", "mp3", "flac", "ogg", "wma", "m4a", "hls", "m3u", "mpeg", "opus",
               "aac+", "aacplus", "streaming", "icecast", "shoutcast"}
_TAG_BLACKLIST = {"24/7", "acir", "acir online", "acapulco", "adulta", "a cappella",
                  "90s y más", "80s en español", "70 80 hits"}
_DECADE_RE = re.compile(r"^(?:19|20)?(\d{2})0?(?:s|'s|er|ies)?$", re.I)


def _tag_decade_key(name: str):
    """年代标签归一化：80s/80's/80er/1980s -> 80s；2000s -> 00s；2010s -> 10s。非年代返回 None。"""
    m = _DECADE_RE.match(name.strip())
    if m:
        d = int(m.group(1))
        if d % 10 == 0:
            return "%02ds" % d
    return None


def _is_junk_tag(name: str) -> bool:
    """判断标签是否垃圾（codec / 频率 / 电台名 / 纯数字），不该建歌单。"""
    n = name.strip()
    ln = n.lower()
    if ln in _CODEC_TAGS or n in _TAG_BLACKLIST:
        return True
    if "@" in n or "http" in n or "://" in n:
        return True
    if re.fullmatch(r"\d+\.\d+\s*(fm|am|mhz|khz)", ln):
        return True
    if re.fullmatch(r"[\d\s.]+", n):
        return True
    if len(n) <= 2:
        return True
    return False


async def _get_tags() -> list:
    """过滤后的标签列表（一天 TTL）：流派白名单前置 + codec/垃圾排除 + 年代去重 + 取前 TAG_TOP_N。"""
    global _tags, _tags_ts
    import time as _time
    now = _time.time()
    if _tags and (now - _tags_ts) < _TAGS_TTL:
        return _tags
    picked: list = []
    allt = await rc.list_tags()
    if allt:
        allt.sort(key=lambda t: int(t.get("stationcount") or 0), reverse=True)
        seen_decade: set = set()
        for t in allt:
            name = str(t.get("name") or "").strip()
            cnt = int(t.get("stationcount") or 0)
            if not name or cnt < TAG_MIN_COUNT:
                continue
            if _is_junk_tag(name):
                continue
            dk = _tag_decade_key(name)
            if dk:
                if dk in seen_decade:
                    continue
                seen_decade.add(dk)
                t = dict(t)
                t["name"] = dk  # 归一化标签名（80er -> 80s）
            picked.append(t)
            if len(picked) >= TAG_TOP_N:
                break
    # 流派白名单前置（核心流派排在年代/adult 之前），stationcount 懒加载（预热/点击时填充）
    pinned: list = []
    for en in PINNED_TAGS:
        if any(str(p.get("name") or "") == en for p in picked):
            continue
        pinned.append({"name": en, "stationcount": 0, "pinned": True})
    if picked or pinned:
        _tags = pinned + picked
        _tags_ts = now
    return _tags


async def _get_tag_stations(tag: str) -> list:
    """按标签拉电台（带缓存 5 分钟 TTL）。"""
    import time as _time
    now = _time.time()
    cached = _tag_stations_cache.get(tag)
    ts = _tag_stations_ts.get(tag, 0.0)
    if cached and (now - ts) < _STATIONS_TTL:
        return cached
    stations = await rc.tag_stations(tag, limit=TAG_LIMIT)
    if stations:
        _tag_stations_cache[tag] = stations
        _tag_stations_ts[tag] = now
    return stations


def _tag_playlist_vo(tag: str) -> dict:
    """标签分类歌单卡片条目。"""
    t = next((x for x in _tags if x.get("name") == tag), None)
    label = PINNED_TAGS.get(tag, tag)
    name = f"{VIRTUAL_PLAYLIST_NAME} · {label}"
    count = int(t.get("stationcount") or 0) if t else 0
    return _playlist_vo(TAG_GUID_PREFIX + tag, name, count, TAG_COVER_ID + "-" + tag)


def _playlist_vo(guid: str, name: str, track_count: int, cover_id: str) -> dict:
    """playlist 卡片/详情 VO（主/国家歌单通用）。"""
    return {
        "guid": guid,
        "name": name,
        "coverId": cover_id,
        "coverUrl": "/music/api/v1/static/cover?coverId=" + quote(cover_id, safe=""),
        "trackCount": track_count,
        "albumCount": 0,
    }


def _find_country(cc: str) -> dict | None:
    for c in _countries:
        if str(c.get("iso_3166_1") or "").lower() == cc.lower():
            return c
    return None


def _virtual_playlist_vo() -> dict:
    """主歌单「全球电台 · 热门」卡片条目。"""
    return _playlist_vo(VIRTUAL_PLAYLIST_GUID, HOT_PLAYLIST_NAME, _cached_radio_count, HOT_COVER_ID)


def _virtual_playlist_detail_vo() -> dict:
    """主歌单「全球电台 · 热门」详情。"""
    return _playlist_vo(VIRTUAL_PLAYLIST_GUID, HOT_PLAYLIST_NAME, _cached_radio_count, HOT_COVER_ID)


def _country_playlist_vo(cc: str) -> dict:
    """国家分类歌单卡片条目。"""
    c = _find_country(cc)
    name = f"{VIRTUAL_PLAYLIST_NAME} · {rc._country_label(str(c.get('name') or ''))}" if c else VIRTUAL_PLAYLIST_NAME
    count = int(c.get("stationcount") or 0) if c else 0
    return _playlist_vo(COUNTRY_GUID_PREFIX + cc, name, count, FLAG_COVER_PREFIX + cc)


def _virtual_playlist_vos() -> list[dict]:
    """全部虚拟歌单卡片：主歌单 + 标签分类(80s~70s disco) + 国家分类(中国置顶)。"""
    vos = [_virtual_playlist_vo()]
    # 标签分类紧跟热门，其次是国家分类
    for t in _tags:
        tag = str(t.get("name") or "").strip()
        if not tag:
            continue
        vos.append(_tag_playlist_vo(tag))
    for c in _countries:
        cc = str(c.get("iso_3166_1") or "").strip()
        if not cc:
            continue
        vos.append(_country_playlist_vo(cc))
    return vos


async def _warm_pinned_tag_counts() -> None:
    """预热白名单流派电台数（后台，供卡片 trackCount 显示），复用 _get_tag_stations 缓存电台。"""
    for en in PINNED_TAGS:
        try:
            stations = await _get_tag_stations(en)
            cnt = len(stations)
            for t in _tags:
                if str(t.get("name") or "") == en:
                    t["stationcount"] = cnt
                    break
        except Exception:
            pass


async def _warm_radio_count() -> None:
    """后台预热：国家列表 + 热门电台 + 流派电台数 + 流连通性预检（不阻塞请求，失败降级下次再试）。"""
    try:
        await _get_countries()
    except Exception:
        pass
    try:
        await _get_tags()
    except Exception:
        pass
    try:
        await _get_radio_stations()
    except Exception:
        pass
    try:
        await _warm_pinned_tag_counts()
    except Exception:
        pass
    # 流连通性预检：填充正/负缓存，供列表过滤被墙电台（放最后，避免拖慢首屏）
    if _cached_stations:
        try:
            await _prewarm_connectivity(_cached_stations)
        except Exception:
            pass


@app.on_event("startup")
async def _startup():
    asyncio.create_task(_warm_radio_count())


def _load_station_overrides() -> None:
    if not STATION_FILE:
        return
    try:
        with open(STATION_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
        if isinstance(data, dict):
            for k, v in data.items():
                if isinstance(v, dict):
                    _stations[str(k)] = v
                    rc.register_sid(str(k))
    except Exception as exc:
        logger.warning("load station file failed: %s", exc)


def filter_headers(headers, exclude_keys: set | None = None) -> dict:
    exclude = HOP_BY_HOP | {k.lower() for k in (exclude_keys or set())}
    out = {}
    for k, v in headers.items():
        if k.lower() in exclude:
            continue
        try:
            str(v).encode("latin-1")
            out[k] = v
        except UnicodeEncodeError:
            out[k] = str(v).encode("latin-1", errors="replace").decode("latin-1")
    return out


def copy_incoming_headers(request: Request) -> dict:
    headers = filter_headers(request.headers, exclude_keys={"host", "content-length"})
    headers["accept-encoding"] = "identity"
    for key in AUTH_HEADERS:
        val = request.headers.get(key)
        if val:
            headers[key] = val
    return headers


def get_upstream_client() -> httpx.AsyncClient:
    client = getattr(app.state, "upstream_client", None)
    if client is None:
        transport = httpx.AsyncHTTPTransport(uds=UPSTREAM_SOCK)
        client = httpx.AsyncClient(transport=transport, base_url="http://unix", timeout=30.0)
        app.state.upstream_client = client
    return client


async def forward_to_upstream(request: Request) -> Response:
    """原样透传官方 upstream（带鉴权头）。"""
    url_path = request.url.path
    if request.url.query:
        url_path = f"{url_path}?{request.url.query}"
    headers = copy_incoming_headers(request)
    try:
        body = await request.body()
    except ClientDisconnect:
        # 客户端已断开（常见于慢请求被前端取消），无需继续，直接返回空响应防 ASGI 崩溃
        return Response(status_code=499)
    except Exception:
        body = b""
    client = get_upstream_client()
    req = client.build_request(
        method=request.method, url=url_path, headers=headers,
        content=body if body else None,
    )
    resp = await client.send(req, stream=True)
    resp_headers = filter_headers(resp.headers, exclude_keys={"content-length", "content-encoding"})
    content_length = resp.headers.get("content-length")
    content_encoding = (resp.headers.get("content-encoding") or "").strip().lower()
    if (content_length and content_encoding in ("", "identity")
            and 200 <= resp.status_code < 300 and resp.status_code not in (204, 304)):
        resp_headers["content-length"] = content_length

    async def body_stream() -> AsyncGenerator[bytes, None]:
        try:
            async for chunk in resp.aiter_bytes():
                yield chunk
        finally:
            await resp.aclose()

    return StreamingResponse(body_stream(), status_code=resp.status_code, headers=resp_headers)


def _extract_keyword(request: Request) -> str:
    q = request.query_params
    return (q.get("keyword") or q.get("q") or q.get("query") or "").strip()



async def _search_radio(keyword: str, limit: int = 20) -> list[dict]:
    """搜 radio-browser，转 track 并缓存 station 供 stream 反查。"""
    stations = await rc.search_stations(keyword, limit=limit)
    for st in stations:
        _stations[str(st.get("stationuuid"))] = st
    return rc.search_to_tracks(stations)


async def _fetch_station_by_uuid(sid: str) -> dict | None:
    """按 stationuuid 查 radio-browser（内存 miss 时兜底）。"""
    return await rc.station_by_uuid(sid)


async def _fetch_upstream(request: Request) -> httpx.Response:
    """透传官方拿原始 httpx 响应（不封装成 StreamingResponse）。"""
    url_path = request.url.path
    if request.url.query:
        url_path = f"{url_path}?{request.url.query}"
    headers = copy_incoming_headers(request)
    body = await request.body()
    client = get_upstream_client()
    req = client.build_request(
        method=request.method, url=url_path, headers=headers,
        content=body if body else None,
    )
    return await client.send(req, stream=True)


def _json_headers(resp: httpx.Response) -> dict:
    return filter_headers(resp.headers, exclude_keys={"content-length", "content-encoding"})


@app.get("/music/api/v1/playlist/list")
@app.get("/music/api/v1/playlist/list/{subpath:path}")
async def playlist_list(request: Request):
    """歌单列表：透传官方 + 注入「全球电台」主歌单 + N 个国家分类歌单卡片。"""
    # 国家/标签列表未预热时同步拉一次，保证首屏就带分类歌单
    if not _countries:
        try:
            await _get_countries()
        except Exception:
            pass
    if not _tags:
        try:
            await _get_tags()
        except Exception:
            pass
    resp = await _fetch_upstream(request)
    try:
        raw = await resp.aread()
    finally:
        await resp.aclose()
    headers = _json_headers(resp)
    try:
        upstream_json = json.loads(raw or b"{}")
    except Exception:
        return Response(content=raw, status_code=resp.status_code, headers=headers)
    if not isinstance(upstream_json, dict) or upstream_json.get("code") != 0:
        return Response(content=raw, status_code=resp.status_code, headers=headers)

    data = upstream_json.get("data")
    if not isinstance(data, dict):
        data = {}
        upstream_json["data"] = data
    lst = data.get("list")
    if not isinstance(lst, list):
        lst = data.get("items") or data.get("playlists") or []
        data["list"] = lst

    # 幂等：已注入过任一虚拟歌单就不重复加
    if any(isinstance(x, dict) and (x.get("guid") == VIRTUAL_PLAYLIST_GUID
            or str(x.get("guid") or "").startswith(COUNTRY_GUID_PREFIX)) for x in lst):
        return Response(content=raw, status_code=resp.status_code, headers=headers)

    added = _virtual_playlist_vos()
    lst.extend(added)
    total = data.get("total", len(lst))
    if isinstance(total, int):
        data["total"] = total + len(added)

    headers["Cache-Control"] = "no-store"  # Q4：防前端缓存，注入卡片即时可见
    return JSONResponse(content=upstream_json, status_code=resp.status_code, headers=headers)


@app.get("/music/api/v1/playlist/detail")
@app.get("/music/api/v1/playlist/detail/{subpath:path}")
async def playlist_detail(request: Request):
    """歌单详情：guid 命中虚拟歌单（主/国家）时返回虚拟歌单，其余透传。"""
    guid = (request.query_params.get("guid")
            or request.query_params.get("playlistGUID")
            or request.query_params.get("playlistId") or "").strip()
    if guid == VIRTUAL_PLAYLIST_GUID:
        return JSONResponse(content={"code": 0, "msg": "", "data": _virtual_playlist_detail_vo()})
    if guid.startswith(TAG_GUID_PREFIX):
        return JSONResponse(content={"code": 0, "msg": "",
                                     "data": _tag_playlist_vo(guid[len(TAG_GUID_PREFIX):])})
    if guid.startswith(COUNTRY_GUID_PREFIX):
        return JSONResponse(content={"code": 0, "msg": "",
                                     "data": _country_playlist_vo(guid[len(COUNTRY_GUID_PREFIX):])})
    return await forward_to_upstream(request)


@app.get("/music/api/v1/track/playlist-detail/list")
@app.get("/music/api/v1/track/playlist-detail/list/{subpath:path}")
async def playlist_detail_list(request: Request):
    """歌单内歌曲列表：playlistGUID 命中虚拟歌单（主/国家）时返回对应电台，其余透传。"""
    guid = (request.query_params.get("playlistGUID")
            or request.query_params.get("playlistId")
            or request.query_params.get("guid") or "").strip()
    if guid == VIRTUAL_PLAYLIST_GUID:
        stations = await _get_radio_stations()
    elif guid.startswith(TAG_GUID_PREFIX):
        stations = await _get_tag_stations(guid[len(TAG_GUID_PREFIX):])
    elif guid.startswith(COUNTRY_GUID_PREFIX):
        stations = await _get_country_stations(guid[len(COUNTRY_GUID_PREFIX):])
    else:
        return await forward_to_upstream(request)

    try:
        page = max(1, int(request.query_params.get("page") or 1))
        size_raw = request.query_params.get("size")
        if size_raw in (None, "", "-1", "0"):
            size = HOT_LIMIT
        else:
            size = max(1, int(size_raw))
    except ValueError:
        page, size = 1, HOT_LIMIT

    for st in stations:
        _stations[str(st.get("stationuuid"))] = st
    tracks = rc.search_to_tracks(stations)
    total = len(tracks)
    start = (page - 1) * size
    page_tracks = tracks[start:start + size]

    return JSONResponse(content={
        "code": 0, "msg": "",
        "data": {"list": page_tracks, "total": total, "page": page, "pageSize": size},
    }, headers={"Cache-Control": "no-store"})


def _hot_cover_response() -> Response:
    """主歌单「全球电台」（热门电台）封面：火焰 PNG。"""
    return Response(content=cover.hot_cover_png(), media_type="image/png",
                    headers={"Cache-Control": "public, max-age=86400"})


def _tag_cover_response(tag: str = "") -> Response:
    """标签分类歌单封面：按标签返回语义图标 PNG。"""
    return Response(content=cover.tag_cover_png(tag), media_type="image/png",
                    headers={"Cache-Control": "public, max-age=86400"})


_flag_cache: dict[str, bytes] = {}
_flag_cover_cache: dict[str, bytes] = {}
_flag_fail: dict[str, float] = {}
_FLAG_FAIL_TTL = 300.0
_FLAG_TIMEOUT = 5.0

# 电台台标（favicon）代理缓存：
#   _favicon_ok:   {sid: (bytes, content_type)}，成功缓存 1 天
#   _favicon_fail: {sid: 失败时间戳}，失败负缓存 5 分钟（避免每个台标反复超时拖慢列表）
_favicon_ok: dict[str, tuple[bytes, str]] = {}
_favicon_fail: dict[str, float] = {}
_FAVICON_OK_TTL = 86400.0
_FAVICON_FAIL_TTL = 300.0
_FAVICON_TIMEOUT = 4.0


async def _favicon_response(sid: str) -> Response:
    """电台台标：带缓存 + 失败兜底默认图（绝不透传官方，避免 400 与 ASGI 崩溃）。"""
    import time as _time
    now = _time.time()
    hit = _favicon_ok.get(sid)
    if hit is not None:
        return Response(content=hit[0], media_type=hit[1],
                        headers={"Cache-Control": "public, max-age=86400"})
    fail_ts = _favicon_fail.get(sid, 0.0)
    if now - fail_ts < _FAVICON_FAIL_TTL:
        return Response(content=_default_cover(), media_type="image/png",
                        headers={"Cache-Control": "public, max-age=86400"})

    favicon = rc.favicon_for(sid)
    if not favicon:
        _favicon_fail[sid] = now
        return Response(content=_default_cover(), media_type="image/png",
                        headers={"Cache-Control": "public, max-age=86400"})
    try:
        async with httpx.AsyncClient(timeout=_FAVICON_TIMEOUT, follow_redirects=True,
                                     trust_env=False) as client:
            resp = await client.get(favicon, headers={"User-Agent": "fnradio/1.0"})
            if resp.status_code == 200 and resp.content:
                ct = (resp.headers.get("content-type") or "image/png").split(";")[0].strip()
                _favicon_ok[sid] = (resp.content, ct)
                return Response(content=resp.content, media_type=ct,
                                headers={"Cache-Control": "public, max-age=86400"})
    except Exception as exc:
        logger.info("favicon fetch failed for %s: %s", sid, type(exc).__name__)
    _favicon_fail[sid] = now
    return Response(content=_default_cover(), media_type="image/png",
                    headers={"Cache-Control": "public, max-age=86400"})


def _flag_cover_response(cc: str, flag_png: bytes) -> Response:
    """国家歌单封面：圆形国旗合成透明底 PNG。"""
    cached = _flag_cover_cache.get(cc)
    if cached is None:
        cached = cover.flag_cover_png(cc, flag_png)
        _flag_cover_cache[cc] = cached
    return Response(content=cached, media_type="image/png",
                    headers={"Cache-Control": "public, max-age=86400"})


async def _get_flag_response(cc: str) -> Response:
    """下载 circle-flags 圆形国旗 SVG 并渲染成 PNG（带缓存 + 失败负缓存），合成透明底封面返回。"""
    cc = cc.lower()
    png = _flag_cache.get(cc)
    import time as _time
    now = _time.time()
    if png is None and (now - _flag_fail.get(cc, 0.0)) >= _FLAG_FAIL_TTL:
        try:
            async with httpx.AsyncClient(timeout=_FLAG_TIMEOUT, trust_env=False,
                                         follow_redirects=True) as client:
                resp = await client.get(f"{FLAGCDN}/{cc}.svg",
                                        headers={"User-Agent": "fnradio/1.0"})
                if resp.status_code == 200 and resp.content:
                    rendered = cover.render_flag_svg(resp.content)
                    if rendered:
                        png = rendered
                        _flag_cache[cc] = png
                    else:
                        png = b""
                else:
                    png = b""
        except Exception as exc:
            logger.info("flag fetch failed for %s: %s", cc, type(exc).__name__)
            png = b""
        if not png:
            _flag_fail[cc] = now
    if not png:
        return Response(content=_default_cover(), media_type="image/png",
                        headers={"Cache-Control": "public, max-age=86400"})
    return _flag_cover_response(cc, png)


@app.get("/music/api/v1/static/cover")
@app.get("/music/api/v1/static/cover/{subpath:path}")
async def static_cover(request: Request):
    """电台台标代理：radio-hot 返热门图标；radio-flag-<ISO> 返国旗；radio-<uuid> 返电台 favicon；其余透传。"""
    cover_id = (request.query_params.get("coverId") or "").strip()
    if cover_id == HOT_COVER_ID:
        return _hot_cover_response()
    if cover_id == TAG_COVER_ID or cover_id.startswith(TAG_COVER_ID + "-"):
        return _tag_cover_response(cover_id[len(TAG_COVER_ID):].lstrip("-"))
    if cover_id.startswith(FLAG_COVER_PREFIX):
        return await _get_flag_response(cover_id[len(FLAG_COVER_PREFIX):])
    if not cover_id.startswith("radio-"):
        return await forward_to_upstream(request)
    return await _favicon_response(cover_id[len("radio-"):])


@app.get("/music/api/v1/search/track")
@app.get("/music/api/v1/search/track/{subpath:path}")
async def search_track(request: Request):
    keyword = _extract_keyword(request)
    resp = await _fetch_upstream(request)
    try:
        raw = await resp.aread()
    finally:
        await resp.aclose()
    headers = _json_headers(resp)
    # 无 keyword 或官方非 200：原样返回官方响应
    if not keyword or resp.status_code != 200:
        return Response(content=raw, status_code=resp.status_code, headers=headers)
    try:
        upstream_json = json.loads(raw or b"{}")
    except Exception:
        return Response(content=raw, status_code=resp.status_code, headers=headers)
    if not isinstance(upstream_json, dict) or upstream_json.get("code") != 0:
        return Response(content=raw, status_code=resp.status_code, headers=headers,
                        media_type="application/json")

    data = upstream_json.get("data")
    if not isinstance(data, dict):
        data = {}
        upstream_json["data"] = data
    target_list = data.get("list")
    if not isinstance(target_list, list):
        target_list = data.get("items") or data.get("tracks") or data.get("records") or []
        data["list"] = target_list

    # 合并电台（去重：同名同艺术家跳过）
    existing = {(t.get("title"), t.get("artist")) for t in target_list if isinstance(t, dict)}
    radio_tracks = await _search_radio(keyword)
    added = 0
    for t in radio_tracks:
        if (t.get("title"), t.get("artist")) in existing:
            continue
        target_list.append(t)
        added += 1
    orig_total = data.get("total", len(target_list))
    if isinstance(orig_total, int):
        data["total"] = orig_total + added

    return JSONResponse(content=upstream_json, status_code=resp.status_code, headers=headers)


@app.api_route("/music/api/v1/track/stream", methods=["GET", "HEAD"])
@app.api_route("/music/api/v1/track/stream/{subpath:path}", methods=["GET", "HEAD"])
async def stream_track(request: Request, subpath: str = ""):
    raw_guid = (request.query_params.get("guid") or request.query_params.get("id")
                or request.query_params.get("trackId") or "").strip()
    if subpath:
        raw_guid = raw_guid or subpath
    _load_station_overrides()  # 先加载覆盖文件，再反解 guid（否则注入的 sid 反解不出）
    real_guid = rc.resolve_guid(raw_guid)
    logger.info("stream_track: raw=%s subpath=%s real=%s", raw_guid, subpath, real_guid)
    if not real_guid.startswith(rc.GUID_PREFIX):
        # 本地曲库 / 其他在线 guid：透传官方
        logger.info("stream_track: 非电台 guid，透传官方")
        return await forward_to_upstream(request)

    sid = real_guid[len(rc.GUID_PREFIX):]
    station = _stations.get(sid) or await _fetch_station_by_uuid(sid)
    if not station:
        return JSONResponse(content={"code": 99999, "msg": "STATION NOT FOUND", "data": None},
                            status_code=404)
    stream_url = rc._station_stream_url(station)
    if not stream_url:
        return JSONResponse(content={"code": 99999, "msg": "NO STREAM URL", "data": None},
                            status_code=502)
    return await _proxy_live_stream(stream_url)


async def _proxy_live_stream(stream_url: str) -> Response:
    """流式代理电台直播流：无 Content-Length、无 seek、不落盘。"""
    headers = {"Accept-Encoding": "identity", "User-Agent": "fnradio/1.0"}
    # 强制 IPv4 直连（避开本机 IPv6 出站黑洞），HTTP 流用 IP + Host header
    url_ip, extra_headers = rc.force_ipv4_url(stream_url)
    if extra_headers:
        headers.update(extra_headers)
    try:
        upstream_req = httpx.Request("GET", url_ip, headers=headers)
        client = httpx.AsyncClient(timeout=httpx.Timeout(connect=6.0, read=60.0, write=30.0, pool=10.0),
                                   follow_redirects=True, trust_env=False)
        resp = await client.send(upstream_req, stream=True)
    except Exception as exc:
        # 记录连通性负缓存：NAS 连不上这个流 host，列表下次过滤掉它，避免连续跳过
        _host = rc.stream_host(stream_url)
        if _host:
            import time as _time
            _conn_fail[_host] = _time.time()
            _conn_ok.pop(_host, None)
        logger.info("live stream open failed for %s: %s — fallback 302 redirect", stream_url, type(exc).__name__)
        # 国外流媒体 NAS 直连超时（被墙），回退 302 让客户端（用户设备）直连流 URL：
        # 用户设备与 NAS 网络出口不同，往往能连上这些流。国内电台 NAS 可直连，仍走上面的流式代理。
        return RedirectResponse(stream_url, status_code=302)

    # NAS 成功连上流：记录连通性正缓存（后续预检/过滤复用）
    _host = rc.stream_host(stream_url)
    if _host:
        import time as _time
        _conn_ok[_host] = _time.time()
        _conn_fail.pop(_host, None)

    ct = resp.headers.get("content-type") or "audio/mpeg"
    # 直播流：剥 Content-Length / Content-Range / Accept-Ranges，避免播放器当定长文件 seek
    out_headers = {
        "Content-Type": ct,
        "Cache-Control": "no-store",
        "Accept-Ranges": "none",
    }

    async def body_stream() -> AsyncGenerator[bytes, None]:
        try:
            async for chunk in resp.aiter_bytes():
                yield chunk
        finally:
            await client.aclose()

    return StreamingResponse(body_stream(), status_code=200, headers=out_headers)


@app.get("/_ext/healthz")
@app.get("/_ext/livez")
async def healthz():
    return JSONResponse(content={"service": "fnradio", "status": "ok"})


@app.api_route("/{path:path}", methods=["GET", "POST", "PUT", "DELETE", "HEAD", "PATCH", "OPTIONS"])
async def catch_all(request: Request):
    return await forward_to_upstream(request)

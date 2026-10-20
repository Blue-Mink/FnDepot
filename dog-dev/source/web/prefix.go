package web

import (
	"context"
	"strings"
)

// 飞牛统一网关（gatewaySocket / gatewayPrefix）访问时，平台把
// /app/<appname>/* 原样转发到应用自己的 unix socket——**前缀不会被剥掉**。
// 因此应用侧必须：
//
//  1. 请求进入时把前缀剥掉再交给内部路由（见 api.GatewayHandler）；
//  2. 返回 index.html 时把站内绝对地址（/assets/**、/favicon.png）
//     补上前缀，否则浏览器会去请求 NAS 根路径下的同名资源而 404。
//
// 前缀通过请求上下文传递，直连端口访问时上下文里没有前缀，行为与从前完全一致。

type prefixKey struct{}

// WithPrefix 把统一网关访问前缀放进上下文。
func WithPrefix(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, prefixKey{}, normalizePrefix(prefix))
}

// PrefixFrom 取出上下文里的前缀；未设置时返回空串（直连端口访问）。
func PrefixFrom(ctx context.Context) string {
	if v, ok := ctx.Value(prefixKey{}).(string); ok {
		return v
	}
	return ""
}

// normalizePrefix 统一前缀形态：以 / 开头、无尾斜杠。
func normalizePrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimSuffix(p, "/")
}

// rewriteIndex 给 index.html 里的站内绝对地址补上网关前缀。
//
// 只处理构建产物里真实出现的两类引用：/assets/**（脚本、样式、预加载）
// 与 /favicon.png。前端代码里的其它站内地址（接口、logo、/ca 等）
// 由前端运行时按 location.pathname 自行拼前缀，见 web/ui/src/base.js。
func rewriteIndex(data []byte, prefix string) []byte {
	if prefix == "" {
		return data
	}
	s := string(data)
	s = strings.ReplaceAll(s, `="/assets/`, `="`+prefix+`/assets/`)
	s = strings.ReplaceAll(s, `="/favicon.png`, `="`+prefix+`/favicon.png`)
	return []byte(s)
}

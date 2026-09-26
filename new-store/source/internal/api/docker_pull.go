package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"fnos-store/internal/config"
)

// pullAbortNeedles marks pull failures that no mirror switch can fix: local
// conditions (cancellation, disk full, OOM kill) under which every further
// attempt fails the same way. Anything else — allowlist denials, access
// denied, missing manifests, network errors — is per-mirror, so the chain
// advances to the next candidate.
var pullAbortNeedles = []string{
	"context canceled",
	"context deadline exceeded",
	"no space left on device",
	"signal: killed",
}

// isPullAbortError reports whether a failed pull aborts the whole fallback
// chain (true) or falls through to the next mirror candidate (false).
func isPullAbortError(output string, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := output
	if err != nil {
		msg += "\n" + err.Error()
	}
	for _, needle := range pullAbortNeedles {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// dockerPullCandidates builds the ordered refs to try for one compose image:
// the selected mirror's shape first (including its registry-rewrite refs),
// every other real mirror re-prefixed by ITS OWN multi-registry capability,
// other mirrors' registry-rewrite refs, then the bare direct ref last
// (conversun/fnos-apps#267, #266, #257, #248; ghcr rewrite: #286).
// Duplicates collapse — e.g. a single-registry mirror cannot proxy a
// non-docker.io registry and yields the direct ref, which the chain already
// carries.
func dockerPullCandidates(composeRef string, cfg config.Config) []string {
	canonical := stripDockerMirrorPrefix(composeRef, cfg)
	candidates := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(ref string) {
		if _, dup := seen[ref]; dup {
			return
		}
		seen[ref] = struct{}{}
		candidates = append(candidates, ref)
	}

	// Rewrite mirrors for the SELECTED mirror lead the chain...
	for _, m := range config.DockerMirrorOptions() {
		if m.Key != cfg.DockerMirror {
			continue
		}
		for _, ref := range config.RewriteRefsForMirror(m, canonical) {
			add(ref)
		}
	}
	// auto 模式：本地 KSpeeder 的改写同样置顶（本地缓存毫秒级；
	// 已降级=连续探测失败时跳过，让云镜像顶上）
	if cfg.DockerMirror == "auto" && !config.DockerSmartDegraded("kspeeder") {
		if m, ok := config.DockerMirrorByKey("kspeeder"); ok {
			for _, ref := range config.RewriteRefsForMirror(m, canonical) {
				add(ref)
			}
		}
	}

	prefixes := config.DockerFallbackPrefixes(cfg.DockerMirror, cfg)
	// ...other mirrors' rewrites slot in before the trailing direct ref, so a
	// ghcr-capable mirror outranks a hopeless direct ghcr.io pull.
	trailingDirect := len(prefixes) > 0 && prefixes[len(prefixes)-1] == ""
	body := prefixes
	if trailingDirect {
		body = prefixes[:len(prefixes)-1]
	}
	for _, prefix := range body {
		ref := applyDockerMirrorPrefix(canonical, prefix)
		// A single-registry mirror cannot proxy a non-docker.io registry and
		// collapses onto the bare canonical ref. Skip that placeholder here so
		// the rewrite mirrors below outrank the direct pull, which closes the
		// chain where it belongs.
		if ref != canonical || !trailingDirect {
			add(ref)
		}
	}
	for _, m := range config.DockerMirrorOptions() {
		if m.Key == cfg.DockerMirror {
			continue
		}
		// 本地 KSpeeder 已降级（连续探测失败）时不再占链位
		if m.Key == "kspeeder" && config.DockerSmartDegraded("kspeeder") {
			continue
		}
		for _, ref := range config.RewriteRefsForMirror(m, canonical) {
			add(ref)
		}
	}
	if trailingDirect {
		add(canonical)
	}
	return candidates
}

// stripDockerMirrorPrefix removes whichever known mirror prefix composeRef
// carries — the configured one first, then any other — leaving the canonical
// ref each candidate re-prefixes from scratch.
func stripDockerMirrorPrefix(image string, cfg config.Config) string {
	// 本地 KSpeeder ref 还原为 docker.io canonical。两种形态：
	//  1. 标准 registry 路径：127.0.0.1:5443/library/busybox（官方镜像带
	//     library/ 段，需剥掉后补 docker.io/）
	//  2. 模板拼接：compose 用 ${DOCKER_MIRROR}docker.io/x/y 时，KSPEEDER
	//     前缀后已是完整 canonical（rest 以 docker.io/ 开头，原样返回，
	//     不能再补 docker.io/，否则双重化）
	if strings.HasPrefix(image, config.KSpeederPrefix) {
		rest := image[len(config.KSpeederPrefix):]
		if strings.HasPrefix(rest, "docker.io/") {
			return rest
		}
		if strings.HasPrefix(rest, "library/") {
			rest = rest[len("library/"):]
		}
		return "docker.io/" + rest
	}
	if selected := config.DockerMirrorPrefix(cfg.DockerMirror, cfg); selected != "" && strings.HasPrefix(image, selected) {
		return image[len(selected):]
	}
	for _, m := range config.DockerMirrorOptions() {
		if m.URL != "" && strings.HasPrefix(image, m.URL) {
			return image[len(m.URL):]
		}
	}
	return image
}

// applyDockerMirrorPrefix prefixes a canonical ref for one mirror, honoring
// that mirror's multi-registry capability with the exact semantics of
// normalizeImageForPull: multi-registry proxies keep the registry in the
// path, single-registry proxies strip docker.io/ and cannot proxy other
// registries at all (those refs pass through unprefixed).
func applyDockerMirrorPrefix(image, prefix string) string {
	return normalizeImageForPull(prefix+image, prefix, dockerPrefixMultiRegistry(prefix))
}

// dockerPrefixMultiRegistry resolves a prefix URL to its mirror's capability.
// The custom prefix and "" are not in the table and read as single-registry,
// matching IsDockerMirrorMultiRegistry("custom"/"direct").
func dockerPrefixMultiRegistry(prefix string) bool {
	if prefix == "" {
		return false
	}
	for _, m := range config.DockerMirrorOptions() {
		if m.URL == prefix {
			return m.MultiRegistry
		}
	}
	return false
}

// pullImageWithFallback walks the candidate refs, one attempt each: mirror
// denials advance with an SSE notice, fatal local errors abort immediately.
// It returns the ref that pulled successfully so the caller can tag it back
// onto the compose ref, or ONE aggregated error naming every source tried.
func (p *installPipeline) pullImageWithFallback(ctx context.Context, stream pipelineSink, candidates []string, message string) (string, error) {
	attempts := make([]string, 0, len(candidates))
	for i, ref := range candidates {
		if i > 0 {
			_ = stream.sendProgress(progressPayload{
				Step:    "pulling",
				Message: fmt.Sprintf("镜像源拉取失败，正在尝试备用镜像源 (%d/%d)...", i+1, len(candidates)),
			})
		}
		output, err := p.pullSingleImage(ctx, stream, ref, message)
		if err == nil {
			return ref, nil
		}
		detail := output
		if detail == "" {
			detail = err.Error()
		}
		if isPullAbortError(detail, err) {
			return "", fmt.Errorf("Docker 镜像拉取失败: %s\n请尝试在 Docker 设置中更换镜像加速源后重试", detail)
		}
		attempts = append(attempts, ref+": "+detail)
	}
	return "", fmt.Errorf("Docker 镜像拉取失败: 已尝试 %d 个镜像源均失败:\n%s\n请尝试在 Docker 设置中更换镜像加速源后重试",
		len(attempts), strings.Join(attempts, "\n"))
}

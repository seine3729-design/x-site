/**
 * 线路域名池（docs/04 F、docs/12 第 6 节）。
 *
 * 主来源是后台存储线路表，随 `GET /api/v1/site-config` 的 `media` 段下发（契约 0.77.0）：
 * `image_bases` 用来把封面等**相对 key** 拼成完整地址，两个池子一起进 CSP 的 `connect-src`。
 *
 * 部署配置（`IMAGE_BASE_URLS`、`MEDIA_CONNECT_ORIGINS`）退为**补充**，不再是唯一来源：
 * - 后台可以把封面 / Logo 填成任意完整地址，那类域名不在线路表里，只能在部署配置里登记；
 * - 后端不可用时站点配置会退到兜底值（没有 `media` 段），此时部署配置兼作兜底。
 *
 * 下发的池子排在前面：它才是「当前后台真正在用的线路」，相对 key 要优先按它拼。
 */

/** 只接受 http(s) 开头的字符串；其余（空串、相对路径、javascript: 等）一律丢掉。 */
const ABSOLUTE = /^https?:\/\/[^\s]+$/i;

/**
 * 合并下发的域名池与部署配置，按顺序去重。
 *
 * `fromConfig` 是 unknown：站点配置是后端下发的数据，前台不能假设它的形状
 * （旧版本后端没有 `media` 段，字段也可能是 null）。
 */
export function mergeDomains(fromConfig: unknown, fromEnv: readonly string[] = []): string[] {
	const out: string[] = [];
	for (const value of [...listOf(fromConfig), ...fromEnv]) {
		const base = value.trim().replace(/\/+$/, '');
		if (ABSOLUTE.test(base) && !out.includes(base)) {
			out.push(base);
		}
	}
	return out;
}

function listOf(value: unknown): string[] {
	return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
}

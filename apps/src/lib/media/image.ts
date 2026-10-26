/**
 * 图片地址解析（docs/12 第 6 节）：接口返回的封面是相对 key（经图片线路拼成完整地址）或后台手填的 http(s) 完整地址。
 *
 * 图片线路的域名目前由部署配置 IMAGE_BASE_URLS 给出（按顺序作为候选，首选失败自动换下一条）；
 * 按地区排序的图片线路（/api/v1/lines?kind=image 带 base_url）接入后由它替换，调用方不用改。
 */
const ABSOLUTE = /^https?:\/\//i;

export function imageSources(key: string | null | undefined, bases: readonly string[]): string[] {
	const value = (key ?? '').trim();
	if (!value) {
		return [];
	}
	if (ABSOLUTE.test(value)) {
		return [value];
	}
	// 协议相对地址与 data:/javascript: 一律不用
	if (value.startsWith('//') || /^[a-z][a-z0-9+.-]*:/i.test(value)) {
		return [];
	}
	const path = value.replace(/^\/+/, '');
	return bases.map((base) => `${base.replace(/\/+$/, '')}/${path}`);
}

/** 从剧名生成稳定的渐变色相（无封面时的占位图），同一部剧每次颜色一样。 */
export function titleHue(title: string): number {
	let hash = 0;
	for (const char of title) {
		hash = (hash * 31 + (char.codePointAt(0) ?? 0)) >>> 0;
	}
	return hash % 360;
}

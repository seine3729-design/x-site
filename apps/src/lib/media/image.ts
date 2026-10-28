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

/**
 * 图片实时缩放（docs/08 第 5 节、docs/04 E）：经站点自己所在的 Cloudflare 区域的 Image Transformations，
 * 地址形如 `/cdn-cgi/image/width=360,…/https://cdn.example/img/x.webp`。
 *
 * 刻意用**相对路径**（与页面同源）：
 * - `BlobImage` 用 `fetch` 取图，Cloudflare 缩放后的响应不带 CORS 头，跨源取一定失败；同源不需要 CORS；
 * - 首屏大图复用页面已经建好的连接，不必再和 CDN 握一次手。
 * 原图仍是 CDN 上的完整地址，只有它的 origin 登记在 `IMAGE_RESIZE_ORIGINS` 里才缩放（没登记的原样用）。
 */
export const RESIZE_PATH = '/cdn-cgi/image';

/**
 * 请求宽度只落在这几档：同一张图在不同屏幕上命中同一份边缘缓存，也压住 Cloudflare 的「唯一变换」数
 * （按「原图 × 尺寸参数」计，格式与质量不算；免费额度每月 5000 个）。
 */
export const IMAGE_WIDTHS = [120, 240, 360, 540, 720, 1080, 1440, 1920] as const;

/** 像素比封顶 2：3 倍屏上 2 倍图肉眼几乎看不出差别，字节数却要多一倍多 */
const MAX_DPR = 2;
/** 实测封面 360 宽：原图 335 KB → WebP 约 40 KB（质量 70）；格式与质量不影响计费 */
const QUALITY = 70;

/**
 * 允许最多放大约 10% 再显示：412 宽 × 1.75 倍屏要 721 像素，差 1 像素就跳到下一档（1080）
 * 要多下一半的字节，而 10% 以内的放大在高倍屏上看不出来。
 */
const UPSCALE_TOLERANCE = 0.9;

/** 显示尺寸（CSS 像素）× 像素比，向上取到最近的档位；超过最大档取最大档。 */
export function pickWidth(cssPixels: number, dpr = 1): number {
	const ratio = Math.min(Math.max(Number.isFinite(dpr) ? dpr : 1, 1), MAX_DPR);
	const target = Math.max(cssPixels, 1) * ratio * UPSCALE_TOLERANCE;
	return IMAGE_WIDTHS.find((width) => width >= target) ?? IMAGE_WIDTHS[IMAGE_WIDTHS.length - 1];
}

/**
 * `auto`：按浏览器的 Accept 选 AVIF / WebP，只适合 `<img>`（浏览器自己会带 Accept）。
 * `webp`：`fetch` 默认的 Accept 不含任何图片格式，auto 会退成 JPEG，所以 BlobImage 固定要 WebP（基线浏览器都能解）。
 */
export type ResizeFormat = 'auto' | 'webp';

export type ResizeRequest = {
	width?: number;
	height?: number;
	format?: ResizeFormat;
	/** 默认 70；首屏首图压着暗色渐变，用 60 再省约 15%（质量不影响计费，也不增加唯一变换数） */
	quality?: number;
};

/**
 * 缩放后的地址；原图不在可缩放的 origin 里、或没给尺寸时返回 null（调用方用原图）。
 * `fit=scale-down` 不放大小图；`onerror=redirect` 让缩放失败（如超出免费额度）时 Cloudflare 302 回原图。
 */
export function resizedUrl(src: string, request: ResizeRequest, origins: readonly string[]): string | null {
	if (origins.length === 0 || (!request.width && !request.height)) {
		return null;
	}
	let url: URL;
	try {
		url = new URL(src);
	} catch {
		return null;
	}
	if (!origins.includes(url.origin)) {
		return null;
	}
	const options = [
		...(request.width ? [`width=${request.width}`] : []),
		...(request.height ? [`height=${request.height}`] : []),
		`quality=${request.quality ?? QUALITY}`,
		`format=${request.format ?? 'auto'}`,
		'fit=scale-down',
		'onerror=redirect'
	];
	return `${RESIZE_PATH}/${options.join(',')}/${url.href}`;
}

/** `<img srcset>` 用的多档地址；任何一档缩放不了就返回空串（调用方只用 src）。 */
export function resizedSrcset(
	src: string,
	widths: readonly number[],
	origins: readonly string[],
	quality?: number
): string {
	const entries: string[] = [];
	for (const width of widths) {
		const url = resizedUrl(src, { width, quality }, origins);
		if (!url) {
			return '';
		}
		entries.push(`${url} ${width}w`);
	}
	return entries.join(', ');
}

/**
 * BlobImage 的候选地址：先试缩放后的（同源），失败再按原图逐条线路试（docs/12 第 6 节的线路回退不变）。
 * request 为 null（不知道显示尺寸，或缩放入口已判定不可用）时只用原图。
 */
export function imageCandidates(
	sources: readonly string[],
	request: ResizeRequest | null,
	origins: readonly string[]
): string[] {
	if (!request) {
		return [...sources];
	}
	for (const source of sources) {
		const resized = resizedUrl(source, request, origins);
		if (resized) {
			return [resized, ...sources];
		}
	}
	return [...sources];
}

/** 从剧名生成稳定的渐变色相（无封面时的占位图），同一部剧每次颜色一样。 */
export function titleHue(title: string): number {
	let hash = 0;
	for (const char of title) {
		hash = (hash * 31 + (char.codePointAt(0) ?? 0)) >>> 0;
	}
	return hash % 360;
}

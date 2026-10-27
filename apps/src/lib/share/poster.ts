/**
 * 分享海报（docs/04 C.7）：Canvas 画「封面 + 剧名 第N集 + 二维码」，可下载。
 * 二维码用仓库内的 `qr.ts`（零依赖）；封面取不到时按剧名画稳定渐变，不留空白。
 */
import { loadBlobUrl } from '#lib/media/blob-image.ts';
import { titleHue } from '#lib/media/image.ts';
import { encodeQR } from './qr.ts';

/** 海报画布尺寸（9:16，分享到社交平台的常用比例） */
export const POSTER_WIDTH = 720;
export const POSTER_HEIGHT = 1280;

export type PosterOptions = {
	/** 剧名 */
	title: string;
	/** 「第 3 集」之类的副标题，详情页分享时可以不传 */
	subtitle?: string;
	/** 站点名，画在顶部 */
	siteName: string;
	/** 二维码指向的完整地址 */
	url: string;
	/** 封面候选地址（多条图片线路），空数组时画渐变 */
	cover: readonly string[];
	/** 「扫码观看」这类提示语 */
	scanHint: string;
};

/** 下载用的文件名：去掉路径不允许的字符，附上日期便于区分 */
export function posterFileName(title: string, subtitle?: string): string {
	const stamp = new Date().toISOString().slice(0, 10);
	const name = [title, subtitle].filter(Boolean).join('-').replace(/[\\/:*?"<>|\s]+/g, '-').slice(0, 48);
	return `${name || 'poster'}-${stamp}.png`;
}

/** 按字宽把文本折行，最多 maxLines 行，超出的末行加省略号 */
export function wrapLines(
	measure: (text: string) => number,
	text: string,
	maxWidth: number,
	maxLines: number
): string[] {
	const chars = [...text];
	const lines: string[] = [];
	let line = '';
	for (const char of chars) {
		const next = line + char;
		if (measure(next) > maxWidth && line !== '') {
			lines.push(line);
			line = char;
			if (lines.length === maxLines) {
				break;
			}
		} else {
			line = next;
		}
	}
	if (lines.length < maxLines && line !== '') {
		lines.push(line);
	}
	if (lines.length === maxLines) {
		const consumed = lines.join('').length;
		if (consumed < text.length) {
			let last = lines[maxLines - 1]!;
			while (last.length > 1 && measure(`${last}…`) > maxWidth) {
				last = last.slice(0, -1);
			}
			lines[maxLines - 1] = `${last}…`;
		}
	}
	return lines;
}

async function loadCover(sources: readonly string[]): Promise<ImageBitmap | null> {
	if (sources.length === 0) {
		return null;
	}
	let url: string | null = null;
	try {
		url = await loadBlobUrl(sources, { priority: 'high' });
		const response = await fetch(url);
		return await createImageBitmap(await response.blob());
	} catch {
		return null;
	} finally {
		if (url) {
			URL.revokeObjectURL(url);
		}
	}
}

/** 等比裁剪填满目标框（object-fit: cover） */
function coverRect(iw: number, ih: number, w: number, h: number) {
	const scale = Math.max(w / iw, h / ih);
	const dw = iw * scale;
	const dh = ih * scale;
	return { x: (w - dw) / 2, y: (h - dh) / 2, w: dw, h: dh };
}

function roundedPath(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
	ctx.beginPath();
	ctx.moveTo(x + r, y);
	ctx.arcTo(x + w, y, x + w, y + h, r);
	ctx.arcTo(x + w, y + h, x, y + h, r);
	ctx.arcTo(x, y + h, x, y, r);
	ctx.arcTo(x, y, x + w, y, r);
	ctx.closePath();
}

/**
 * 把海报画到 canvas 上。颜色取当前主题的 CSS 变量，后台换配色海报跟着变。
 * 在浏览器里调用（用到 Canvas 与 fetch）。
 */
export async function drawPoster(canvas: HTMLCanvasElement, options: PosterOptions): Promise<void> {
	const { title, subtitle, siteName, url, cover, scanHint } = options;
	canvas.width = POSTER_WIDTH;
	canvas.height = POSTER_HEIGHT;
	const ctx = canvas.getContext('2d');
	if (!ctx) {
		throw new Error('canvas 不可用');
	}
	const styles = getComputedStyle(document.documentElement);
	const token = (name: string, fallback: string) => styles.getPropertyValue(name).trim() || fallback;
	const brand = token('--brand', '#e11d48');
	const canvasBg = token('--canvas', '#111118');
	const fg = token('--fg', '#f5f5f7');

	// 底色：页面底色打底 + 品牌色从上方淡淡铺下来
	ctx.fillStyle = canvasBg;
	ctx.fillRect(0, 0, POSTER_WIDTH, POSTER_HEIGHT);
	const wash = ctx.createLinearGradient(0, 0, POSTER_WIDTH, POSTER_HEIGHT);
	wash.addColorStop(0, brand);
	wash.addColorStop(1, canvasBg);
	ctx.globalAlpha = 0.28;
	ctx.fillStyle = wash;
	ctx.fillRect(0, 0, POSTER_WIDTH, POSTER_HEIGHT);
	ctx.globalAlpha = 1;

	// 版面（720×1280 下按固定坐标排，改任何一项都要重算下面的块，别让封面把文字挤出画布）
	const pad = 56;
	const cardW = 440;
	const cardH = Math.round((cardW * 3) / 2); // 2:3 封面
	const cardX = Math.round((POSTER_WIDTH - cardW) / 2);
	const cardY = 116;
	const titleY = cardY + cardH + 70; // 776 + 70
	const titleStep = 56;
	const qrBox = 176;
	const qrX = POSTER_WIDTH - pad - qrBox;
	const qrY = POSTER_HEIGHT - pad - qrBox;

	// 站点名
	ctx.fillStyle = fg;
	ctx.globalAlpha = 0.75;
	ctx.font = '600 30px system-ui, sans-serif';
	ctx.textBaseline = 'middle';
	ctx.fillText(siteName, pad, 64);
	ctx.globalAlpha = 1;

	const bitmap = await loadCover(cover);
	ctx.save();
	roundedPath(ctx, cardX, cardY, cardW, cardH, 28);
	ctx.clip();
	if (bitmap) {
		const rect = coverRect(bitmap.width, bitmap.height, cardW, cardH);
		ctx.drawImage(bitmap, cardX + rect.x, cardY + rect.y, rect.w, rect.h);
		bitmap.close();
	} else {
		// 没有封面：按剧名生成稳定渐变 + 首字，与列表里的占位一致
		const hue = titleHue(title);
		const grad = ctx.createLinearGradient(cardX, cardY, cardX + cardW, cardY + cardH);
		grad.addColorStop(0, `hsl(${hue} 55% 32%)`);
		grad.addColorStop(1, `hsl(${(hue + 40) % 360} 50% 18%)`);
		ctx.fillStyle = grad;
		ctx.fillRect(cardX, cardY, cardW, cardH);
		ctx.fillStyle = 'rgba(255,255,255,0.9)';
		ctx.font = '700 160px system-ui, sans-serif';
		ctx.textAlign = 'center';
		ctx.fillText([...title][0] ?? '剧', cardX + cardW / 2, cardY + cardH / 2);
		ctx.textAlign = 'start';
	}
	ctx.restore();

	// 剧名与集数（在二维码上方，可以用整幅宽度）
	ctx.fillStyle = fg;
	ctx.font = '800 46px system-ui, sans-serif';
	const lines = wrapLines((text) => ctx.measureText(text).width, title, POSTER_WIDTH - pad * 2, 2);
	lines.forEach((line, i) => ctx.fillText(line, pad, titleY + i * titleStep));
	if (subtitle) {
		ctx.globalAlpha = 0.7;
		ctx.font = '500 28px system-ui, sans-serif';
		ctx.fillText(subtitle, pad, titleY + lines.length * titleStep + 10);
		ctx.globalAlpha = 1;
	}

	// 二维码（右下角白底卡片，保证留白够、好扫）
	const qr = encodeQR(url);
	ctx.fillStyle = '#ffffff';
	roundedPath(ctx, qrX, qrY, qrBox, qrBox, 16);
	ctx.fill();
	if (qr) {
		const quiet = 2;
		const scale = (qrBox - 20) / (qr.size + quiet * 2);
		const originX = qrX + 10 + quiet * scale;
		const originY = qrY + 10 + quiet * scale;
		ctx.fillStyle = '#000000';
		for (let row = 0; row < qr.size; row += 1) {
			for (let col = 0; col < qr.size; col += 1) {
				if (qr.modules[row]![col]) {
					// 相邻模块多画 0.5px，避免缩放后出现白缝导致扫不出来
					ctx.fillRect(originX + col * scale, originY + row * scale, scale + 0.5, scale + 0.5);
				}
			}
		}
	}

	// 扫码提示与地址：排在二维码左边，宽度要让开二维码，否则会压到码上
	const textW = qrX - pad - 24;
	ctx.fillStyle = fg;
	ctx.font = '600 28px system-ui, sans-serif';
	ctx.fillText(scanHint, pad, qrY + 38);
	ctx.globalAlpha = 0.55;
	ctx.font = '400 22px system-ui, sans-serif';
	const urlLines = wrapLines((text) => ctx.measureText(text).width, url, textW, 2);
	urlLines.forEach((line, i) => ctx.fillText(line, pad, qrY + 86 + i * 30));
	ctx.globalAlpha = 1;
}

/** 画好后导出 PNG Blob */
export async function posterBlob(canvas: HTMLCanvasElement, options: PosterOptions): Promise<Blob> {
	await drawPoster(canvas, options);
	return new Promise<Blob>((resolve, reject) => {
		canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('导出失败'))), 'image/png');
	});
}

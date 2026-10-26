import { describe, expect, it } from 'vitest';
import { documentSecurityHeaders } from '../../../config/security-headers.ts';

const find = (headers: { key: string; value: string }[], key: string) =>
	headers.find((h) => h.key.toLowerCase() === key.toLowerCase())?.value;

const directive = (csp: string, name: string) =>
	csp
		.split(';')
		.map((part) => part.trim())
		.find((part) => part === name || part.startsWith(`${name} `));

describe('documentSecurityHeaders', () => {
	it('开发环境不带 CSP 与 HSTS（本地是 http，装上会挡掉 Vite 的热更新）', () => {
		const headers = documentSecurityHeaders(false, '', ['https://img.example']);
		expect(find(headers, 'content-security-policy')).toBeUndefined();
		expect(find(headers, 'strict-transport-security')).toBeUndefined();
		expect(find(headers, 'X-Content-Type-Options')).toBe('nosniff');
	});

	it('生产环境带 CSP、HSTS 与点击劫持防护', () => {
		const headers = documentSecurityHeaders(true, '', []);
		expect(find(headers, 'strict-transport-security')).toContain('max-age=31536000');
		expect(find(headers, 'X-Frame-Options')).toBe('DENY');
		const csp = find(headers, 'content-security-policy')!;
		expect(directive(csp, 'default-src')).toBe("default-src 'self'");
		expect(directive(csp, 'object-src')).toBe("object-src 'none'");
		expect(directive(csp, 'frame-ancestors')).toBe("frame-ancestors 'none'");
	});

	it('图片线路域名同时进 img-src 与 connect-src（封面是 fetch 下来再挂 blob: 的）', () => {
		const csp = find(documentSecurityHeaders(true, '', ['https://img1.example/i', 'http://img2.example:18090/x']), 'content-security-policy')!;
		for (const name of ['img-src', 'connect-src']) {
			expect(directive(csp, name)).toContain('https://img1.example');
			expect(directive(csp, name)).toContain('http://img2.example:18090');
		}
		// 只要域名，不带路径（CSP 的来源里带路径会改变匹配语义）
		expect(directive(csp, 'connect-src')).not.toContain('img1.example/i');
		expect(directive(csp, 'connect-src')).not.toContain(':18090/x');
	});

	it('同一个域名的多条线路只写一次', () => {
		const csp = find(documentSecurityHeaders(true, '', ['https://img.example/a', 'https://img.example/b']), 'content-security-policy')!;
		expect(directive(csp, 'connect-src')!.match(/https:\/\/img\.example/g)).toHaveLength(1);
	});

	it('填错的线路地址被忽略，不会写出坏的 CSP（坏值会让整条策略失效）', () => {
		const csp = find(documentSecurityHeaders(true, '', ['', '不是地址', 'javascript:alert(1)', 'https://ok.example']), 'content-security-policy')!;
		expect(directive(csp, 'connect-src')).toBe("connect-src 'self' https://ok.example");
	});

	it('视频切片域名池只进 connect-src（hls.js 用 XHR 取切片），不进 img-src', () => {
		const csp = find(documentSecurityHeaders(true, '', ['https://img.example'], ['https://v1.cdn.example', 'https://v2.cdn.example']), 'content-security-policy')!;
		expect(directive(csp, 'connect-src')).toBe("connect-src 'self' https://img.example https://v1.cdn.example https://v2.cdn.example");
		expect(directive(csp, 'img-src')).toBe("img-src 'self' blob: https://img.example");
		// 画面本身是 MSE 的 blob:，不需要把线路域名写进 media-src
		expect(directive(csp, 'media-src')).toBe("media-src 'self' blob:");
	});

	it('图片与视频线路用同一个域名时不重复列出', () => {
		const csp = find(documentSecurityHeaders(true, '', ['https://cdn.example/img'], ['https://cdn.example/v']), 'content-security-policy')!;
		expect(directive(csp, 'connect-src')).toBe("connect-src 'self' https://cdn.example");
	});

	it('没配图片线路时就是 self（加上播放器要用的 blob:）', () => {
		const csp = find(documentSecurityHeaders(true, '', []), 'content-security-policy')!;
		expect(directive(csp, 'connect-src')).toBe("connect-src 'self'");
		expect(directive(csp, 'img-src')).toBe("img-src 'self' blob:");
		expect(directive(csp, 'media-src')).toBe("media-src 'self' blob:");
		expect(directive(csp, 'worker-src')).toBe("worker-src 'self' blob:");
	});

	it('静态资源 CDN 进脚本、样式、字体与图片来源', () => {
		const csp = find(documentSecurityHeaders(true, 'https://cdn.example/', []), 'content-security-policy')!;
		for (const name of ['script-src', 'style-src', 'font-src', 'img-src']) {
			expect(directive(csp, name)).toContain('https://cdn.example');
		}
		// 末尾斜杠不要带进来
		expect(csp).not.toContain('https://cdn.example/ ');
	});
});

import { describe, expect, it } from 'vitest';
import { imageSources, titleHue } from './image.ts';

const BASES = ['https://img1.example/i', 'https://img2.example/i/'];

describe('imageSources', () => {
	it('相对 key 按线路顺序拼成候选地址', () => {
		expect(imageSources('cover/1.jpg', BASES)).toEqual(['https://img1.example/i/cover/1.jpg', 'https://img2.example/i/cover/1.jpg']);
	});

	it('拼接时不产生多余斜杠', () => {
		expect(imageSources('/cover/1.jpg', ['https://img1.example/i//'])).toEqual(['https://img1.example/i/cover/1.jpg']);
	});

	it('后台手填的完整地址直接用，不拼线路', () => {
		expect(imageSources('https://cdn.example/a.webp', BASES)).toEqual(['https://cdn.example/a.webp']);
		expect(imageSources('HTTP://cdn.example/a.webp', BASES)).toEqual(['HTTP://cdn.example/a.webp']);
	});

	it('空值、没有图片线路时给空数组（调用方显示占位图）', () => {
		expect(imageSources('', BASES)).toEqual([]);
		expect(imageSources('   ', BASES)).toEqual([]);
		expect(imageSources(null, BASES)).toEqual([]);
		expect(imageSources(undefined, BASES)).toEqual([]);
		expect(imageSources('cover/1.jpg', [])).toEqual([]);
	});

	it('协议相对地址与其他协议一律不用（防止注入 javascript:、data:）', () => {
		expect(imageSources('//evil.example/a.jpg', BASES)).toEqual([]);
		expect(imageSources('javascript:alert(1)', BASES)).toEqual([]);
		expect(imageSources('data:image/png;base64,AAAA', BASES)).toEqual([]);
	});
});

describe('titleHue', () => {
	it('同一个剧名每次都是同一个色相', () => {
		expect(titleHue('闪婚后，豪门老公宠我上天')).toBe(titleHue('闪婚后，豪门老公宠我上天'));
	});

	it('色相落在 0–359', () => {
		for (const title of ['A', '重生之千金归来', '', '🎬 短剧', 'a'.repeat(200)]) {
			const hue = titleHue(title);
			expect(hue).toBeGreaterThanOrEqual(0);
			expect(hue).toBeLessThan(360);
			expect(Number.isInteger(hue)).toBe(true);
		}
	});

	it('不同剧名基本会分到不同色相', () => {
		const titles = ['重生之千金归来', '战神归来', '绝世神医', '逆天改命', '龙王赘婿', '穿越之锦绣医妃'];
		expect(new Set(titles.map(titleHue)).size).toBeGreaterThanOrEqual(titles.length - 1);
	});
});

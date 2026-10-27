import { describe, expect, it } from 'vitest';
import { encodeQR, pickVersion, QR_MAX_VERSION } from './qr.ts';

/** 版本 n 的模块边长 */
const sizeOf = (version: number) => 21 + 4 * (version - 1);

describe('版本选择', () => {
	it('按等级 M 的数据容量挑最小版本', () => {
		expect(pickVersion(1)).toBe(1);
		expect(pickVersion(14)).toBe(1); // 版本 1 容量 16 字节，减去 1 字节长度头
		expect(pickVersion(15)).toBe(2);
		expect(pickVersion(26)).toBe(2);
		expect(pickVersion(27)).toBe(3);
		expect(pickVersion(180)).toBe(9);
		expect(pickVersion(181)).toBe(10);
		expect(pickVersion(213)).toBe(10);
	});

	it('超过版本 10 的容量时返回 0（调用方退回只显示链接）', () => {
		expect(pickVersion(214)).toBe(0);
		expect(pickVersion(5000)).toBe(0);
		expect(encodeQR('x'.repeat(5000))).toBeNull();
	});
});

describe('矩阵结构', () => {
	const qr = encodeQR('https://example.com/d/114965?e=1')!;

	it('边长是 21 + 4×(版本-1)', () => {
		expect(qr.size).toBe(sizeOf(3)); // 32 字节装不进版本 2（26 字节）
		expect(qr.modules).toHaveLength(qr.size);
		expect(qr.modules.every((row) => row.length === qr.size)).toBe(true);
	});

	it('三个角有定位图案（7×7 回字）', () => {
		const finder = (r0: number, c0: number) => {
			for (let r = 0; r < 7; r += 1) {
				for (let c = 0; c < 7; c += 1) {
					const want = r === 0 || r === 6 || c === 0 || c === 6 || (r >= 2 && r <= 4 && c >= 2 && c <= 4);
					if (qr.modules[r0 + r]![c0 + c] !== want) {
						return false;
					}
				}
			}
			return true;
		};
		expect(finder(0, 0)).toBe(true);
		expect(finder(0, qr.size - 7)).toBe(true);
		expect(finder(qr.size - 7, 0)).toBe(true);
	});

	it('定位图形是交替的，固定深色模块为深色', () => {
		for (let i = 8; i < qr.size - 8; i += 1) {
			expect(qr.modules[6]![i]).toBe(i % 2 === 0);
			expect(qr.modules[i]![6]).toBe(i % 2 === 0);
		}
		expect(qr.modules[qr.size - 8]![8]).toBe(true);
	});

	it('深色比例落在合理区间（掩码罚分起作用）', () => {
		const dark = qr.modules.flat().filter(Boolean).length;
		const ratio = dark / (qr.size * qr.size);
		expect(ratio).toBeGreaterThan(0.35);
		expect(ratio).toBeLessThan(0.65);
	});
});

describe('各版本都能生成', () => {
	it('版本 1–10 的边长与模块数都对，且不同内容矩阵不同', () => {
		const seen = new Set<string>();
		for (let version = 1; version <= QR_MAX_VERSION; version += 1) {
			// 构造刚好落在这个版本的长度
			let length = 1;
			while (pickVersion(length) < version) {
				length += 1;
			}
			const qr = encodeQR('a'.repeat(length))!;
			expect(qr.size, `版本 ${version}`).toBe(sizeOf(version));
			const key = qr.modules.map((row) => row.map((v) => (v ? 1 : 0)).join('')).join('');
			expect(seen.has(key)).toBe(false);
			seen.add(key);
		}
	});

	it('同一段文本每次编码结果一致', () => {
		const a = encodeQR('https://duanju.example/zh-CN/play/1/2')!;
		const b = encodeQR('https://duanju.example/zh-CN/play/1/2')!;
		expect(a.modules).toEqual(b.modules);
	});
});

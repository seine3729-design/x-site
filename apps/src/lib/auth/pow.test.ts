import { describe, expect, it } from 'vitest';
import { leadingZeroBits, solveChallenge } from './pow.ts';

describe('leadingZeroBits', () => {
	it('按 bit 数（不是字节数）算前导零', () => {
		expect(leadingZeroBits(new Uint8Array([0xff]))).toBe(0);
		expect(leadingZeroBits(new Uint8Array([0x80]))).toBe(0);
		expect(leadingZeroBits(new Uint8Array([0x7f]))).toBe(1);
		expect(leadingZeroBits(new Uint8Array([0x01]))).toBe(7);
		expect(leadingZeroBits(new Uint8Array([0x00, 0x40]))).toBe(9);
		expect(leadingZeroBits(new Uint8Array([0x00, 0x00, 0x08]))).toBe(20);
	});

	it('全零时等于总 bit 数', () => {
		expect(leadingZeroBits(new Uint8Array(4))).toBe(32);
		expect(leadingZeroBits(new Uint8Array())).toBe(0);
	});
});

describe('solveChallenge', () => {
	it('解出来的答案满足 SHA-256(nonce:solution) 的前 difficulty 个 bit 为 0', async () => {
		const nonce = 'test-nonce';
		const difficulty = 10;
		const solution = await solveChallenge(nonce, difficulty);
		const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(`${nonce}:${solution}`));
		expect(leadingZeroBits(new Uint8Array(digest))).toBeGreaterThanOrEqual(difficulty);
	});

	it('难度 0 立刻给出第一个候选', async () => {
		expect(await solveChallenge('n', 0)).toBe('0');
	});

	it('页面离开时能中止，不会一直占着 CPU', async () => {
		const controller = new AbortController();
		controller.abort();
		await expect(solveChallenge('n', 32, controller.signal)).rejects.toThrow();
	});
});

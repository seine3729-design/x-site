import { describe, expect, it } from 'vitest';
import { peekAccessToken, safeNext } from './session.ts';

/** 造一个只有载荷可读的假 JWT（这里不验签，所以签名段随便填） */
function fakeJwt(payload: unknown): string {
	const body = Buffer.from(JSON.stringify(payload), 'utf8').toString('base64url');
	return `eyJhbGciOiJIUzI1NiJ9.${body}.c2ln`;
}

describe('safeNext', () => {
	it('站内路径原样返回', () => {
		expect(safeNext('/me')).toBe('/me');
		expect(safeNext('/play/1/2?from=home')).toBe('/play/1/2?from=home');
	});

	it('外站地址、协议相对地址、反斜杠绕过都落回兜底路径', () => {
		for (const value of ['https://evil.example', '//evil.example', '/\\evil.example', 'javascript:alert(1)', 'me']) {
			expect(safeNext(value)).toBe('/');
		}
	});

	it('不往接口路径跳（那里不是页面）', () => {
		expect(safeNext('/api/v1/me/state')).toBe('/');
	});

	it('空值用兜底路径，兜底可以自己指定', () => {
		expect(safeNext(null)).toBe('/');
		expect(safeNext(undefined)).toBe('/');
		expect(safeNext('', '/me')).toBe('/me');
		expect(safeNext('https://evil.example', '/me')).toBe('/me');
	});
});

describe('peekAccessToken', () => {
	it('读出 exp 与 role', () => {
		expect(peekAccessToken(fakeJwt({ exp: 1_800_000_000, role: 'user' }))).toEqual({ exp: 1_800_000_000, role: 'user' });
		expect(peekAccessToken(fakeJwt({ exp: 1, role: 'guest' }))).toEqual({ exp: 1, role: 'guest' });
	});

	it('格式不对、载荷不是 JSON、字段缺失或取值意外时都按「没有令牌」处理', () => {
		expect(peekAccessToken('')).toBeNull();
		expect(peekAccessToken('not-a-jwt')).toBeNull();
		expect(peekAccessToken('a.!!!.c')).toBeNull();
		expect(peekAccessToken(fakeJwt({ role: 'user' }))).toBeNull();
		expect(peekAccessToken(fakeJwt({ exp: '1800000000', role: 'user' }))).toBeNull();
		expect(peekAccessToken(fakeJwt({ exp: 1, role: 'admin' }))).toBeNull();
	});
});

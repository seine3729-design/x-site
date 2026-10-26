import { readFileSync, writeFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import {
	ENC_CONTENT_TYPE,
	createSecureClient,
	openResponse,
	sealRequest,
	type FetchLike
} from './api-client.ts';
import { concat, fromBase64url, fromHex, toHex, utf8Bytes, type Bytes } from './hpke.ts';

type Fixture = {
	secret: string;
	kid: string;
	public_key: string;
	ephemeral_private_key_hex: string;
	ts: number;
	method: string;
	request_target: string;
	request_plaintext: string;
	x_enc: string;
	request_body_hex: string;
	response_plaintext: string;
	response_body_hex: string;
};

// 三端共用的互通样本（Go 侧 service/internal/crypto/apienc/interop_test.go 校验同一份内容）
const fixtureUrl = new URL('./testdata/interop_v1.json', import.meta.url);
const fixture = JSON.parse(readFileSync(fixtureUrl, 'utf8')) as Fixture;

async function sealResponseForFixture(key: Bytes, plaintext: string): Promise<Bytes> {
	const nonce = new Uint8Array(12).fill(0x24);
	const aesKey = await crypto.subtle.importKey('raw', key, 'AES-GCM', false, ['encrypt']);
	const ct = await crypto.subtle.encrypt(
		{ name: 'AES-GCM', iv: nonce, additionalData: utf8Bytes('xsite-api/v1 response') },
		aesKey,
		utf8Bytes(plaintext)
	);
	return concat(nonce, new Uint8Array(ct));
}

describe('三端互通样本', () => {
	it('请求封装与样本逐字节一致，样本响应可解密', async () => {
		const sealed = await sealRequest(
			{ kid: fixture.kid, publicKey: fromBase64url(fixture.public_key) },
			fixture.method,
			fixture.request_target,
			fixture.ts,
			utf8Bytes(fixture.request_plaintext),
			fromHex(fixture.ephemeral_private_key_hex)
		);
		if (process.env.UPDATE_FIXTURES === '1') {
			fixture.x_enc = sealed.header;
			fixture.request_body_hex = toHex(sealed.body!);
			fixture.response_body_hex = toHex(await sealResponseForFixture(sealed.responseKey, fixture.response_plaintext));
			writeFileSync(fixtureUrl, JSON.stringify(fixture, null, 2) + '\n');
		}
		expect(sealed.header).toBe(fixture.x_enc);
		expect(toHex(sealed.body!)).toBe(fixture.request_body_hex);
		const plain = await openResponse(sealed.responseKey, fromHex(fixture.response_body_hex));
		expect(new TextDecoder().decode(plain)).toBe(fixture.response_plaintext);
	});

	it('响应被篡改时解密失败', async () => {
		const sealed = await sealRequest(
			{ kid: fixture.kid, publicKey: fromBase64url(fixture.public_key) },
			fixture.method,
			fixture.request_target,
			fixture.ts,
			null,
			fromHex(fixture.ephemeral_private_key_hex)
		);
		const tampered = fromHex(fixture.response_body_hex);
		tampered[tampered.length - 1]! ^= 1;
		await expect(openResponse(sealed.responseKey, tampered)).rejects.toThrow();
	});
});

/** 模拟后端：记录收到的请求，按脚本依次返回响应。 */
function fakeBackend(script: ((req: Request) => Response)[]) {
	const seen: Request[] = [];
	let keyFetches = 0;
	const impl: FetchLike = async (input, init) => {
		const req = new Request(input, init);
		if (new URL(req.url).pathname === '/api/v1/crypto/keys') {
			keyFetches += 1;
			return Response.json({
				code: 'OK',
				data: { current: fixture.kid, keys: [{ kid: fixture.kid, public_key: fixture.public_key }] }
			});
		}
		seen.push(req);
		const next = script.shift();
		if (!next) throw new Error('意外的请求');
		return next(req);
	};
	return { impl, seen, keyFetches: () => keyFetches };
}

const plainError = (code: string, headers: HeadersInit = {}) =>
	Response.json({ code, message: '' }, { status: 400, headers });

describe('createSecureClient', () => {
	it('请求带 X-Enc 头并加密请求体，不在网络上出现明文', async () => {
		const backend = fakeBackend([() => new Response(null, { status: 204 })]);
		const client = createSecureClient({ baseUrl: 'http://bff.test', fetch: backend.impl });
		const res = await client.secureFetch('/api/v1/me/history?from=web', {
			method: 'POST',
			body: '{"episode_id":1}'
		});
		expect(res.status).toBe(204);
		const sent = backend.seen[0]!;
		expect(sent.headers.get('x-enc')).toMatch(/^1\.k1\.\d+\.[\w-]{43}\.[\w-]{22}$/);
		expect(sent.headers.get('content-type')).toBe(ENC_CONTENT_TYPE);
		const wire = new Uint8Array(await sent.arrayBuffer());
		expect(new TextDecoder().decode(wire)).not.toContain('episode_id');
	});

	it('公钥已轮换（ENC_KEY_UNKNOWN）时重新拉公钥并重试一次', async () => {
		const backend = fakeBackend([() => plainError('ENC_KEY_UNKNOWN'), () => Response.json({ code: 'OK', data: 1 })]);
		const client = createSecureClient({ baseUrl: 'http://bff.test', fetch: backend.impl });
		await expect(client.json<number>('/api/v1/x')).resolves.toBe(1);
		expect(backend.keyFetches()).toBe(2);
		expect(backend.seen).toHaveLength(2);
	});

	it('时间偏差（ENC_TIME_SKEW）时按服务端 Date 校准后重试', async () => {
		const serverNow = Date.parse('2026-10-21T12:00:00Z');
		const backend = fakeBackend([
			() => plainError('ENC_TIME_SKEW', { date: new Date(serverNow).toUTCString() }),
			() => Response.json({ code: 'OK', data: 'ok' })
		]);
		// 客户端时钟慢了 1 小时
		const client = createSecureClient({ baseUrl: 'http://bff.test', fetch: backend.impl, now: () => serverNow - 3_600_000 });
		await client.json('/api/v1/x');
		const ts = (r: Request) => Number(r.headers.get('x-enc')!.split('.')[2]);
		expect(ts(backend.seen[0]!)).toBe((serverNow - 3_600_000) / 1000);
		expect(ts(backend.seen[1]!)).toBe(serverNow / 1000);
	});

	it('业务错误按 code 抛出，且只重试协议错误', async () => {
		const backend = fakeBackend([
			() => Response.json({ code: 'NEED_LOGIN', message: '请先登录' }, { status: 401 })
		]);
		const client = createSecureClient({ baseUrl: 'http://bff.test', fetch: backend.impl });
		await expect(client.json('/api/v1/me/state')).rejects.toMatchObject({ status: 401, code: 'NEED_LOGIN' });
		expect(backend.seen).toHaveLength(1);
	});
});

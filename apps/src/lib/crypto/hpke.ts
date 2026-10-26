/**
 * HPKE（RFC 9180）发送方，base 模式，套件固定为
 * DHKEM(X25519, HKDF-SHA256) + HKDF-SHA256 + AES-256-GCM，与服务端 Go 标准库 crypto/hpke 一致。
 *
 * 只用 Web Crypto（浏览器与 Bun 都有），不引入第三方加密库。Web Crypto 的 HKDF 不能单独做
 * Extract / Expand，这里按 RFC 5869 用 HMAC-SHA256 实现 LabeledExtract / LabeledExpand。
 */

export type Bytes = Uint8Array<ArrayBuffer>;

const KEM_ID = 0x0020;
const KDF_ID = 0x0001;
const AEAD_ID = 0x0002;
const NK = 32;
const NN = 12;
const NH = 32;

const utf8 = new TextEncoder();

export function concat(...parts: Uint8Array[]): Bytes {
	const out = new Uint8Array(parts.reduce((n, p) => n + p.byteLength, 0));
	let offset = 0;
	for (const p of parts) {
		out.set(p, offset);
		offset += p.byteLength;
	}
	return out;
}

function i2osp(n: number, length: number): Bytes {
	const out = new Uint8Array(length);
	for (let i = length - 1; i >= 0 && n > 0; i -= 1) {
		out[i] = n & 0xff;
		n = Math.floor(n / 256);
	}
	return out;
}

const text = (s: string): Bytes => utf8.encode(s) as Bytes;

const HPKE_V1 = text('HPKE-v1');
const SUITE_KEM = concat(text('KEM'), i2osp(KEM_ID, 2));
const SUITE_HPKE = concat(text('HPKE'), i2osp(KEM_ID, 2), i2osp(KDF_ID, 2), i2osp(AEAD_ID, 2));
// X25519 私钥的 PKCS#8 固定前缀（RFC 8410），后接 32 字节私钥
const PKCS8_X25519 = Uint8Array.from([
	0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x04, 0x22, 0x04, 0x20
]);
const BASE_POINT = (() => {
	const p = new Uint8Array(32);
	p[0] = 9;
	return p;
})();

async function hmac(key: Bytes, data: Bytes): Promise<Bytes> {
	// HKDF-Extract 的空 salt 等价于 HashLen 个 0；Web Crypto 不接受零长度 HMAC 密钥
	const raw = key.byteLength === 0 ? new Uint8Array(NH) : key;
	const k = await crypto.subtle.importKey('raw', raw, { name: 'HMAC', hash: 'SHA-256' }, false, [
		'sign'
	]);
	return new Uint8Array(await crypto.subtle.sign('HMAC', k, data));
}

function labeledExtract(suite: Bytes, salt: Bytes, label: string, ikm: Bytes): Promise<Bytes> {
	return hmac(salt, concat(HPKE_V1, suite, text(label), ikm));
}

async function labeledExpand(
	suite: Bytes,
	prk: Bytes,
	label: string,
	info: Bytes,
	length: number
): Promise<Bytes> {
	const labeledInfo = concat(i2osp(length, 2), HPKE_V1, suite, text(label), info);
	const out = new Uint8Array(length);
	let block: Bytes = new Uint8Array(0);
	for (let offset = 0, counter = 1; offset < length; counter += 1) {
		block = await hmac(prk, concat(block, labeledInfo, Uint8Array.of(counter)));
		out.set(block.subarray(0, Math.min(block.byteLength, length - offset)), offset);
		offset += block.byteLength;
	}
	return out;
}

async function x25519Ephemeral(fixedPrivateKey?: Bytes): Promise<{ privateKey: CryptoKey; publicKey: Bytes }> {
	if (fixedPrivateKey) {
		// 只用于测试：固定临时私钥，让输出可与 RFC 向量和三端样本逐字节比对
		const privateKey = await crypto.subtle.importKey(
			'pkcs8',
			concat(PKCS8_X25519, fixedPrivateKey),
			{ name: 'X25519' },
			false,
			['deriveBits']
		);
		const base = await crypto.subtle.importKey('raw', BASE_POINT, { name: 'X25519' }, false, []);
		const publicKey = new Uint8Array(
			await crypto.subtle.deriveBits({ name: 'X25519', public: base }, privateKey, 256)
		);
		return { privateKey, publicKey };
	}
	const pair = (await crypto.subtle.generateKey({ name: 'X25519' }, true, ['deriveBits'])) as CryptoKeyPair;
	const publicKey = new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey));
	return { privateKey: pair.privateKey, publicKey };
}

/** HPKE 发送方上下文。Seal 按调用顺序递增序号，必须与接收方 Open 的顺序一致。 */
export class SenderContext {
	#seq = 0;

	constructor(
		readonly enc: Bytes,
		private readonly key: CryptoKey,
		private readonly baseNonce: Bytes,
		private readonly exporterSecret: Bytes
	) {}

	async seal(aad: Bytes, plaintext: Bytes): Promise<Bytes> {
		const nonce = this.baseNonce.slice();
		const seq = i2osp(this.#seq, NN);
		for (let i = 0; i < NN; i += 1) {
			nonce[i]! ^= seq[i]!;
		}
		this.#seq += 1;
		return new Uint8Array(
			await crypto.subtle.encrypt({ name: 'AES-GCM', iv: nonce, additionalData: aad }, this.key, plaintext)
		);
	}

	export(exporterContext: Bytes, length: number): Promise<Bytes> {
		return labeledExpand(SUITE_HPKE, this.exporterSecret, 'sec', exporterContext, length);
	}
}

/** SetupBaseS：向接收方公钥 pkR 封装，返回 enc 与发送方上下文。 */
export async function setupBaseSender(
	recipientPublicKey: Bytes,
	info: Bytes,
	fixedEphemeralKey?: Bytes
): Promise<SenderContext> {
	const eph = await x25519Ephemeral(fixedEphemeralKey);
	const pkR = await crypto.subtle.importKey('raw', recipientPublicKey, { name: 'X25519' }, false, []);
	const dh = new Uint8Array(await crypto.subtle.deriveBits({ name: 'X25519', public: pkR }, eph.privateKey, 256));

	const kemContext = concat(eph.publicKey, recipientPublicKey);
	const eaePrk = await labeledExtract(SUITE_KEM, new Uint8Array(0), 'eae_prk', dh);
	const sharedSecret = await labeledExpand(SUITE_KEM, eaePrk, 'shared_secret', kemContext, NH);

	const empty = new Uint8Array(0);
	const pskIdHash = await labeledExtract(SUITE_HPKE, empty, 'psk_id_hash', empty);
	const infoHash = await labeledExtract(SUITE_HPKE, empty, 'info_hash', info);
	const ksc = concat(Uint8Array.of(0), pskIdHash, infoHash);
	const secret = await labeledExtract(SUITE_HPKE, sharedSecret, 'secret', empty);
	const keyBytes = await labeledExpand(SUITE_HPKE, secret, 'key', ksc, NK);
	const baseNonce = await labeledExpand(SUITE_HPKE, secret, 'base_nonce', ksc, NN);
	const exporterSecret = await labeledExpand(SUITE_HPKE, secret, 'exp', ksc, NH);

	const key = await crypto.subtle.importKey('raw', keyBytes, 'AES-GCM', false, ['encrypt']);
	return new SenderContext(eph.publicKey, key, baseNonce, exporterSecret);
}

export function base64url(bytes: Uint8Array): string {
	let bin = '';
	for (const b of bytes) {
		bin += String.fromCharCode(b);
	}
	return btoa(bin).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
}

export function fromBase64url(value: string): Bytes {
	const b64 = value.replaceAll('-', '+').replaceAll('_', '/');
	const bin = atob(b64 + '='.repeat((4 - (b64.length % 4)) % 4));
	return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

export function fromHex(hex: string): Bytes {
	const out = new Uint8Array(hex.length / 2);
	for (let i = 0; i < out.length; i += 1) {
		out[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
	}
	return out;
}

export function toHex(bytes: Uint8Array): string {
	return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

export { text as utf8Bytes };

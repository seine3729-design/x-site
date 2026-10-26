import { describe, expect, it } from 'vitest';
import { fromHex, setupBaseSender, toHex } from './hpke.ts';

// RFC 9180 官方测试向量：mode_base, DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, AES-256-GCM
// （与 service/internal/crypto/apienc/apienc_test.go、mobile/test/hpke_test.dart 使用同一组）
const V = {
	info: '4f6465206f6e2061204772656369616e2055726e',
	skEm: '179d4b53b6365c45b600c4163b61d95cbc2f4d9e36f1695558dce265ab8bab11',
	pkEm: '6c93e09869df3402d7bf231bf540fadd35cd56be14f97178f0954db94b7fc256',
	pkRm: '430f4b9859665145a6b1ba274024487bd66f03a2dd577d7753c68d7d7d00c00c',
	pt: '4265617574792069732074727574682c20747275746820626561757479',
	encryptions: [
		{
			aad: '436f756e742d30',
			ct: 'e5d84cd531cfb583096e7cfa9641bd3079cf3a91cda813c52deb5f512be9931980a41de125a925cdad859d5b7a'
		},
		{
			aad: '436f756e742d31',
			ct: '2c43aff25343fdbff864506f0818b9d87df84ea01b1a2144d23b4d40c26bf655fdf197fe40297a8aebeed5cc2d'
		}
	],
	exports: [
		{ ctx: '', value: 'ded6cffafaea6b812cbf3e241e88332adbc077aca81512914213810ee291770a' },
		{ ctx: '00', value: '04d3cb6cc116b28ffd22ad5bc276c60d31fec71ceb87ae24db811c64b7507339' }
	]
};

describe('HPKE 发送方', () => {
	it('与 RFC 9180 测试向量逐字节一致', async () => {
		const ctx = await setupBaseSender(fromHex(V.pkRm), fromHex(V.info), fromHex(V.skEm));
		expect(toHex(ctx.enc)).toBe(V.pkEm);
		// 连续 Seal 使用递增序号
		for (const e of V.encryptions) {
			expect(toHex(await ctx.seal(fromHex(e.aad), fromHex(V.pt)))).toBe(e.ct);
		}
		for (const e of V.exports) {
			expect(toHex(await ctx.export(fromHex(e.ctx), 32))).toBe(e.value);
		}
	});

	it('不固定临时私钥时每次封装都不同', async () => {
		const a = await setupBaseSender(fromHex(V.pkRm), fromHex(V.info));
		const b = await setupBaseSender(fromHex(V.pkRm), fromHex(V.info));
		expect(toHex(a.enc)).not.toBe(toHex(b.enc));
	});
});

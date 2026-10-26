/**
 * 人机验证（S2-2，service/internal/pow）：找一个 solution，使 SHA-256(nonce + ":" + solution) 的前 difficulty 个 bit 为 0。
 * 难度 18 平均要算约 26 万次哈希；按批并行调用 Web Crypto，每批之间让出主线程，页面不卡。
 */
export function leadingZeroBits(bytes: Uint8Array): number {
	let count = 0;
	for (const byte of bytes) {
		if (byte === 0) {
			count += 8;
			continue;
		}
		return count + Math.clz32(byte) - 24;
	}
	return count;
}

const BATCH = 512;

export async function solveChallenge(nonce: string, difficulty: number, signal?: AbortSignal): Promise<string> {
	const encoder = new TextEncoder();
	for (let base = 0; base < 50_000_000; base += BATCH) {
		signal?.throwIfAborted();
		const digests = await Promise.all(
			Array.from({ length: BATCH }, (_, k) => crypto.subtle.digest('SHA-256', encoder.encode(`${nonce}:${base + k}`)))
		);
		for (let k = 0; k < digests.length; k += 1) {
			if (leadingZeroBits(new Uint8Array(digests[k]!)) >= difficulty) {
				return String(base + k);
			}
		}
		await new Promise((resolve) => setTimeout(resolve, 0));
	}
	throw new Error('人机验证求解超出上限');
}

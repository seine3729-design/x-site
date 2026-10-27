/**
 * 最小二维码编码器：Model 2、字节模式、纠错等级 M、版本 1–10（最多 216 字节，够放分享链接）。
 *
 * 不引三方库：`apps` 目前零运行时依赖，二维码只在分享海报里用一次、算法是固定标准，
 * 照 `#lib/ui/icons.ts` 的做法把需要的表放进仓库（JIS X 0510 / ISO/IEC 18004）。
 * 正确性由 `qr.test.ts`（结构与容量）加浏览器实测（用 Chrome 的 BarcodeDetector 回读文本）保证。
 */

/** 每个版本（1..10）在等级 M 下的分块：[每块纠错码字数, 一组块数, 一组数据码字数, 二组块数, 二组数据码字数] */
const BLOCKS_M: readonly (readonly [number, number, number, number, number])[] = [
	[10, 1, 16, 0, 0],
	[16, 1, 28, 0, 0],
	[26, 1, 44, 0, 0],
	[18, 2, 32, 0, 0],
	[24, 2, 43, 0, 0],
	[16, 4, 27, 0, 0],
	[18, 4, 31, 0, 0],
	[22, 2, 38, 2, 39],
	[22, 3, 36, 2, 37],
	[26, 4, 43, 1, 44]
];

/** 对齐图案中心坐标（版本 1 没有对齐图案） */
const ALIGN: readonly (readonly number[])[] = [
	[],
	[6, 18],
	[6, 22],
	[6, 26],
	[6, 30],
	[6, 34],
	[6, 22, 38],
	[6, 24, 42],
	[6, 26, 46],
	[6, 28, 50]
];

/** 等级 M 的格式信息位 */
const EC_FORMAT_BITS = 0b00;
/** 字节模式指示符 */
const MODE_BYTE = 0b0100;
/** 填充码字（交替使用） */
const PAD = [0xec, 0x11];

export const QR_MAX_VERSION = BLOCKS_M.length;

// ---------- GF(256) ----------
const EXP = new Uint8Array(512);
const LOG = new Uint8Array(256);
{
	let x = 1;
	for (let i = 0; i < 255; i += 1) {
		EXP[i] = x;
		LOG[x] = i;
		x <<= 1;
		if (x & 0x100) {
			x ^= 0x11d; // 本原多项式 x^8+x^4+x^3+x^2+1
		}
	}
	for (let i = 255; i < 512; i += 1) {
		EXP[i] = EXP[i - 255]!;
	}
}

function mul(a: number, b: number): number {
	return a === 0 || b === 0 ? 0 : EXP[LOG[a]! + LOG[b]!]!;
}

/** 生成多项式 (x-α^0)(x-α^1)…(x-α^(degree-1))，系数由高次到低次 */
function generator(degree: number): number[] {
	let poly = [1];
	for (let i = 0; i < degree; i += 1) {
		const next = new Array<number>(poly.length + 1).fill(0);
		for (let j = 0; j < poly.length; j += 1) {
			next[j] = next[j]! ^ poly[j]!;
			next[j + 1] = next[j + 1]! ^ mul(poly[j]!, EXP[i]!);
		}
		poly = next;
	}
	return poly;
}

/** 一个数据块的纠错码字（Reed-Solomon 余式） */
function errorCorrection(data: readonly number[], ecLen: number): number[] {
	const gen = generator(ecLen);
	const buf = [...data, ...new Array<number>(ecLen).fill(0)];
	for (let i = 0; i < data.length; i += 1) {
		const coef = buf[i]!;
		if (coef === 0) {
			continue;
		}
		for (let j = 1; j < gen.length; j += 1) {
			buf[i + j] = buf[i + j]! ^ mul(gen[j]!, coef);
		}
	}
	return buf.slice(data.length);
}

// ---------- 数据编码 ----------
function dataCapacity(version: number): number {
	const [, g1, d1, g2, d2] = BLOCKS_M[version - 1]!;
	return g1 * d1 + g2 * d2;
}

/** 放得下这串字节的最小版本；超出 216 字节返回 0 */
export function pickVersion(byteLength: number): number {
	for (let version = 1; version <= QR_MAX_VERSION; version += 1) {
		const header = 4 + (version <= 9 ? 8 : 16);
		if (header + byteLength * 8 <= dataCapacity(version) * 8) {
			return version;
		}
	}
	return 0;
}

/** 数据码字 + 纠错码字，按块交错后的最终码字序列 */
function codewords(bytes: Uint8Array, version: number): number[] {
	const [ecLen, g1, d1, g2, d2] = BLOCKS_M[version - 1]!;
	const total = dataCapacity(version);
	const bits: number[] = [];
	const push = (value: number, len: number) => {
		for (let i = len - 1; i >= 0; i -= 1) {
			bits.push((value >> i) & 1);
		}
	};
	push(MODE_BYTE, 4);
	push(bytes.length, version <= 9 ? 8 : 16);
	for (const byte of bytes) {
		push(byte, 8);
	}
	// 终止符最多 4 个 0，再补到字节边界
	for (let i = 0; i < 4 && bits.length < total * 8; i += 1) {
		bits.push(0);
	}
	while (bits.length % 8 !== 0) {
		bits.push(0);
	}
	const words: number[] = [];
	for (let i = 0; i < bits.length; i += 8) {
		words.push(bits.slice(i, i + 8).reduce((acc, bit) => (acc << 1) | bit, 0));
	}
	for (let i = 0; words.length < total; i += 1) {
		words.push(PAD[i % 2]!);
	}

	const blocks: number[][] = [];
	let at = 0;
	for (let i = 0; i < g1; i += 1) {
		blocks.push(words.slice(at, at + d1));
		at += d1;
	}
	for (let i = 0; i < g2; i += 1) {
		blocks.push(words.slice(at, at + d2));
		at += d2;
	}
	const eccs = blocks.map((block) => errorCorrection(block, ecLen));

	const out: number[] = [];
	const widest = Math.max(d1, d2);
	for (let i = 0; i < widest; i += 1) {
		for (const block of blocks) {
			if (i < block.length) {
				out.push(block[i]!);
			}
		}
	}
	for (let i = 0; i < ecLen; i += 1) {
		for (const ecc of eccs) {
			out.push(ecc[i]!);
		}
	}
	return out;
}

// ---------- 掩码与格式信息 ----------
function masked(mask: number, row: number, col: number): boolean {
	switch (mask) {
		case 0:
			return (row + col) % 2 === 0;
		case 1:
			return row % 2 === 0;
		case 2:
			return col % 3 === 0;
		case 3:
			return (row + col) % 3 === 0;
		case 4:
			return (Math.floor(row / 2) + Math.floor(col / 3)) % 2 === 0;
		case 5:
			return ((row * col) % 2) + ((row * col) % 3) === 0;
		case 6:
			return (((row * col) % 2) + ((row * col) % 3)) % 2 === 0;
		default:
			return (((row + col) % 2) + ((row * col) % 3)) % 2 === 0;
	}
}

function formatBits(mask: number): number {
	const data = (EC_FORMAT_BITS << 3) | mask;
	let rem = data;
	for (let i = 0; i < 10; i += 1) {
		rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
	}
	return ((data << 10) | (rem & 0x3ff)) ^ 0x5412;
}

function versionInfoBits(version: number): number {
	let rem = version;
	for (let i = 0; i < 12; i += 1) {
		rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
	}
	return (version << 12) | (rem & 0xfff);
}

// ---------- 矩阵 ----------
type Grid = { size: number; dark: Uint8Array; fixed: Uint8Array };

const idx = (g: Grid, row: number, col: number) => row * g.size + col;

function drawFunctionPatterns(g: Grid, version: number) {
	const n = g.size;
	const set = (row: number, col: number, on: boolean) => {
		if (row < 0 || col < 0 || row >= n || col >= n) {
			return;
		}
		g.dark[idx(g, row, col)] = on ? 1 : 0;
		g.fixed[idx(g, row, col)] = 1;
	};

	// 定位图案 + 分隔符
	for (const [r0, c0] of [
		[0, 0],
		[0, n - 7],
		[n - 7, 0]
	] as const) {
		for (let r = -1; r <= 7; r += 1) {
			for (let c = -1; c <= 7; c += 1) {
				const inner = r >= 0 && r <= 6 && c >= 0 && c <= 6;
				const on = inner && (r === 0 || r === 6 || c === 0 || c === 6 || (r >= 2 && r <= 4 && c >= 2 && c <= 4));
				set(r0 + r, c0 + c, on);
			}
		}
	}
	// 定位图形（timing）
	for (let i = 8; i < n - 8; i += 1) {
		set(6, i, i % 2 === 0);
		set(i, 6, i % 2 === 0);
	}
	// 对齐图案
	const centers = ALIGN[version - 1]!;
	for (const r0 of centers) {
		for (const c0 of centers) {
			const nearFinder = (r0 <= 8 && c0 <= 8) || (r0 <= 8 && c0 >= n - 9) || (r0 >= n - 9 && c0 <= 8);
			if (nearFinder) {
				continue;
			}
			for (let r = -2; r <= 2; r += 1) {
				for (let c = -2; c <= 2; c += 1) {
					set(r0 + r, c0 + c, Math.max(Math.abs(r), Math.abs(c)) !== 1);
				}
			}
		}
	}
	// 格式信息区先占位（真实值在选好掩码后写），以及固定的深色模块
	drawFormat(g, 0);
	set(n - 8, 8, true);
	// 版本信息区（版本 7 起）
	if (version >= 7) {
		const bits = versionInfoBits(version);
		for (let i = 0; i < 18; i += 1) {
			const on = ((bits >> i) & 1) === 1;
			const a = n - 11 + (i % 3);
			const b = Math.floor(i / 3);
			set(b, a, on);
			set(a, b, on);
		}
	}
}

function drawFormat(g: Grid, mask: number) {
	const n = g.size;
	const bits = formatBits(mask);
	const set = (row: number, col: number, on: boolean) => {
		g.dark[idx(g, row, col)] = on ? 1 : 0;
		g.fixed[idx(g, row, col)] = 1;
	};
	const bit = (i: number) => ((bits >> i) & 1) === 1;
	// 第一份：左上角
	for (let i = 0; i <= 5; i += 1) {
		set(i, 8, bit(i));
	}
	set(7, 8, bit(6));
	set(8, 8, bit(7));
	set(8, 7, bit(8));
	for (let i = 9; i < 15; i += 1) {
		set(8, 14 - i, bit(i));
	}
	// 第二份：右上角与左下角
	for (let i = 0; i < 8; i += 1) {
		set(8, n - 1 - i, bit(i));
	}
	for (let i = 8; i < 15; i += 1) {
		set(n - 15 + i, 8, bit(i));
	}
}

function drawCodewords(g: Grid, words: readonly number[]) {
	const n = g.size;
	let bitIndex = 0;
	const nextBit = () => {
		const i = bitIndex;
		bitIndex += 1;
		const word = words[i >> 3];
		return word === undefined ? 0 : (word >> (7 - (i & 7))) & 1;
	};
	for (let col = n - 1; col >= 1; col -= 2) {
		if (col === 6) {
			col = 5; // 第 6 列整列是定位图形，跳过
		}
		// 方向由列号的奇偶推出（不要用「每列取反」的开关：跳过第 6 列后两者会对不上）
		const upward = ((col + 1) & 2) === 0;
		for (let i = 0; i < n; i += 1) {
			const row = upward ? n - 1 - i : i;
			for (const c of [col, col - 1]) {
				if (g.fixed[idx(g, row, c)]) {
					continue;
				}
				g.dark[idx(g, row, c)] = nextBit();
			}
		}
	}
}

function applyMask(g: Grid, mask: number) {
	for (let row = 0; row < g.size; row += 1) {
		for (let col = 0; col < g.size; col += 1) {
			if (!g.fixed[idx(g, row, col)] && masked(mask, row, col)) {
				g.dark[idx(g, row, col)] ^= 1;
			}
		}
	}
}

/** 掩码罚分（标准四条规则）：分数最低的掩码观感最均匀、最好扫 */
function penalty(g: Grid): number {
	const n = g.size;
	const at = (row: number, col: number) => g.dark[idx(g, row, col)] === 1;
	let score = 0;

	// 规则 1：同色连续 5 个以上
	for (let i = 0; i < n; i += 1) {
		for (const horizontal of [true, false]) {
			let run = 1;
			for (let j = 1; j < n; j += 1) {
				const same = horizontal ? at(i, j) === at(i, j - 1) : at(j, i) === at(j - 1, i);
				if (same) {
					run += 1;
				} else {
					if (run >= 5) {
						score += 3 + (run - 5);
					}
					run = 1;
				}
			}
			if (run >= 5) {
				score += 3 + (run - 5);
			}
		}
	}
	// 规则 2：2×2 同色块
	for (let row = 0; row < n - 1; row += 1) {
		for (let col = 0; col < n - 1; col += 1) {
			const v = at(row, col);
			if (v === at(row, col + 1) && v === at(row + 1, col) && v === at(row + 1, col + 1)) {
				score += 3;
			}
		}
	}
	// 规则 3：形似定位图案的 1:1:3:1:1 + 4 空白
	const FINDER = [true, false, true, true, true, false, true];
	const matches = (get: (k: number) => boolean, start: number) => {
		for (let k = 0; k < 7; k += 1) {
			if (get(start + k) !== FINDER[k]) {
				return false;
			}
		}
		const blankBefore = [-4, -3, -2, -1].every((d) => start + d < 0 || !get(start + d));
		const blankAfter = [7, 8, 9, 10].every((d) => start + d >= n || !get(start + d));
		return blankBefore || blankAfter;
	};
	for (let i = 0; i < n; i += 1) {
		for (let start = 0; start <= n - 7; start += 1) {
			if (matches((k) => at(i, k), start)) {
				score += 40;
			}
			if (matches((k) => at(k, i), start)) {
				score += 40;
			}
		}
	}
	// 规则 4：深色比例偏离 50%
	let dark = 0;
	for (let i = 0; i < g.dark.length; i += 1) {
		dark += g.dark[i]!;
	}
	const ratio = (dark * 100) / (n * n);
	score += Math.floor(Math.abs(ratio - 50) / 5) * 10;
	return score;
}

/**
 * 把文本编码成二维码模块矩阵（`true` 为深色）。文本按 UTF-8 编码；
 * 超过版本 10 的容量（216 字节）返回 null，调用方应退回「只显示链接文字」。
 */
export function encodeQR(text: string): { size: number; modules: boolean[][] } | null {
	const bytes = new TextEncoder().encode(text);
	const version = pickVersion(bytes.length);
	if (version === 0) {
		return null;
	}
	const size = 21 + 4 * (version - 1);
	const words = codewords(bytes, version);

	let best: Grid | null = null;
	let bestScore = Number.POSITIVE_INFINITY;
	for (let mask = 0; mask < 8; mask += 1) {
		const g: Grid = { size, dark: new Uint8Array(size * size), fixed: new Uint8Array(size * size) };
		drawFunctionPatterns(g, version);
		drawCodewords(g, words);
		applyMask(g, mask);
		drawFormat(g, mask);
		const score = penalty(g);
		if (score < bestScore) {
			bestScore = score;
			best = g;
		}
	}
	const grid = best!;
	const modules: boolean[][] = [];
	for (let row = 0; row < size; row += 1) {
		const line: boolean[] = [];
		for (let col = 0; col < size; col += 1) {
			line.push(grid.dark[idx(grid, row, col)] === 1);
		}
		modules.push(line);
	}
	return { size, modules };
}

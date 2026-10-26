import { describe, expect, it } from 'vitest';
import { clock, compactNumber, relativeTime, score, shortDate, startOfDay } from './format.ts';

describe('compactNumber', () => {
	it('中文用万 / 亿，英文用 K / M', () => {
		expect(compactNumber(4_630_000, 'zh-CN')).toBe('463万');
		expect(compactNumber(12_300, 'zh-CN')).toBe('1.2万');
		expect(compactNumber(1_200, 'en')).toBe('1.2K');
		expect(compactNumber(3_400_000, 'en')).toBe('3.4M');
	});

	it('小数字原样显示，负数按 0 处理', () => {
		expect(compactNumber(0, 'zh-CN')).toBe('0');
		expect(compactNumber(999, 'en')).toBe('999');
		expect(compactNumber(-5, 'en')).toBe('0');
	});
});

describe('clock', () => {
	it('一小时内是 m:ss', () => {
		expect(clock(0)).toBe('0:00');
		expect(clock(9)).toBe('0:09');
		expect(clock(75)).toBe('1:15');
		expect(clock(599)).toBe('9:59');
	});

	it('超过一小时是 h:mm:ss，分钟补零', () => {
		expect(clock(3600)).toBe('1:00:00');
		expect(clock(3725)).toBe('1:02:05');
	});

	it('负数、小数取整；NaN 与 Infinity 兜成 0:00（元数据还没到时的 video.duration）', () => {
		expect(clock(-3)).toBe('0:00');
		expect(clock(12.7)).toBe('0:12');
		expect(clock(Number.NaN)).toBe('0:00');
		expect(clock(Number.POSITIVE_INFINITY)).toBe('0:00');
	});
});

describe('relativeTime', () => {
	const now = Date.parse('2026-03-10T12:00:00Z');

	it('按秒、分、小时、天选单位', () => {
		expect(relativeTime(now - 30_000, 'zh-CN', now)).toBe('30秒钟前');
		expect(relativeTime(now - 5 * 60_000, 'zh-CN', now)).toBe('5分钟前');
		expect(relativeTime(now - 3 * 3_600_000, 'zh-CN', now)).toBe('3小时前');
		expect(relativeTime(now - 2 * 86_400_000, 'zh-CN', now)).toBe('前天');
	});

	it('超过 30 天改成日期', () => {
		expect(relativeTime(now - 200 * 86_400_000, 'zh-CN', now)).toBe(shortDate(now - 200 * 86_400_000, 'zh-CN'));
	});

	it('接口返回的 ISO 字符串、时间戳、Date 都认', () => {
		expect(relativeTime('2026-03-10T11:00:00Z', 'zh-CN', now)).toBe('1小时前');
		expect(relativeTime(new Date(now - 3_600_000), 'zh-CN', now)).toBe('1小时前');
	});

	it('时间不可解析时返回空串，界面上就不显示这一行', () => {
		expect(relativeTime('不是时间', 'zh-CN', now)).toBe('');
		expect(relativeTime(Number.NaN, 'zh-CN', now)).toBe('');
	});
});

describe('startOfDay', () => {
	it('给出本地时区当天 0 点', () => {
		const start = startOfDay(Date.parse('2026-03-10T15:30:00'));
		const date = new Date(start);
		expect([date.getHours(), date.getMinutes(), date.getSeconds(), date.getMilliseconds()]).toEqual([0, 0, 0, 0]);
		expect(date.getDate()).toBe(new Date(Date.parse('2026-03-10T15:30:00')).getDate());
	});
});

describe('score', () => {
	it('保留一位小数，没有评分时返回 null', () => {
		expect(score(9)).toBe('9.0');
		expect(score(8.75)).toBe('8.8');
		expect(score(null)).toBeNull();
		expect(score(undefined)).toBeNull();
		expect(score(Number.NaN)).toBeNull();
	});
});

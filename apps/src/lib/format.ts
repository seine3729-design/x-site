/**
 * 数字、时长、日期格式化：一律按当前界面语言用 Intl 格式化（docs/04 A.4.2）。
 * Intl 实例创建成本不低，按语言缓存。
 */
const compactCache = new Map<string, Intl.NumberFormat>();
const relativeCache = new Map<string, Intl.RelativeTimeFormat>();
const dateCache = new Map<string, Intl.DateTimeFormat>();

/** 播放量、热度等：中文 1.2万 / 3.4亿，英文 1.2K / 3.4M */
export function compactNumber(value: number, locale: string): string {
	let format = compactCache.get(locale);
	if (!format) {
		format = new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 });
		compactCache.set(locale, format);
	}
	return format.format(Math.max(0, value));
}

/**
 * 秒 → m:ss 或 h:mm:ss（播放器时间、分集时长）。
 * 直播流、元数据还没到时 video.duration 是 NaN / Infinity，这里兜成 0:00，不要让界面出现 NaN:NaN。
 */
export function clock(totalSeconds: number): string {
	const sec = Number.isFinite(totalSeconds) ? Math.max(0, Math.floor(totalSeconds)) : 0;
	const h = Math.floor(sec / 3600);
	const m = Math.floor((sec % 3600) / 60);
	const s = sec % 60;
	const mm = h > 0 ? String(m).padStart(2, '0') : String(m);
	return `${h > 0 ? `${h}:` : ''}${mm}:${String(s).padStart(2, '0')}`;
}

/** 相对时间（「3 分钟前」「昨天」）；超过 30 天显示日期 */
export function relativeTime(input: string | number | Date, locale: string, now = Date.now()): string {
	const time = input instanceof Date ? input.getTime() : typeof input === 'number' ? input : Date.parse(input);
	if (!Number.isFinite(time)) {
		return '';
	}
	const diff = (time - now) / 1000;
	const abs = Math.abs(diff);
	if (abs > 30 * 86_400) {
		return shortDate(time, locale);
	}
	let format = relativeCache.get(locale);
	if (!format) {
		format = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
		relativeCache.set(locale, format);
	}
	if (abs < 60) {
		return format.format(Math.round(diff), 'second');
	}
	if (abs < 3600) {
		return format.format(Math.round(diff / 60), 'minute');
	}
	if (abs < 86_400) {
		return format.format(Math.round(diff / 3600), 'hour');
	}
	return format.format(Math.round(diff / 86_400), 'day');
}

export function shortDate(input: string | number | Date, locale: string): string {
	let format = dateCache.get(locale);
	if (!format) {
		format = new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'short', day: 'numeric' });
		dateCache.set(locale, format);
	}
	return format.format(new Date(input));
}

/** 本地时区当天 0 点的毫秒时间戳（观看历史按「今天 / 昨天 / 更早」分组） */
export function startOfDay(now: number): number {
	const date = new Date(now);
	date.setHours(0, 0, 0, 0);
	return date.getTime();
}

/** 评分保留一位小数（9 → 9.0） */
export function score(value: number | null | undefined): string | null {
	return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(1) : null;
}
